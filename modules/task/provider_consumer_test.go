package task_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

func TestPolicyProvidersPreserveTheTaskConsumer(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		for _, world := range []string{"fake", "sql", "http"} {
			t.Run(adapter+"/"+world, func(t *testing.T) {
				tasktest.RunPolicy(t, func(t *testing.T, run func(tasktest.PolicyFixture)) {
					p := newPolicyLeg(t, adapter)
					ctx := tasktest.As(tenancy.WithTenant(t.Context(), acme), uuid.New())
					var f tasktest.Fixture
					if world == "fake" {
						fake := tasktest.NewFake()
						fake.Policy = p
						f = tasktest.Fixture{Ctx: ctx, Service: fake, Published: fake.Events.Names,
							Seed: func(row *contracts.Task) uuid.UUID { return fake.Put(ctx, row) },
							Task: func(id uuid.UUID) (contracts.Task, error) { return fake.Task(ctx, id) }}
					} else {
						f = sqlPolicyFixture(t, ctx, p, world == "http")
					}
					run(tasktest.PolicyFixture{Fixture: f, Use: p.use, Calls: p.calls, Direct: world != "http"})
				})
			})
		}
	}
}

func sqlPolicyFixture(t *testing.T, ctx context.Context, policy tenancy.Policy, httpWorld bool) tasktest.Fixture {
	t.Helper()
	admin, conn := dbtest.Schema(t, task.Migrations)
	return policySQLWorld(t, ctx, policy, httpWorld, admin, conn)
}

func policySQLWorld(t *testing.T, ctx context.Context, policy tenancy.Policy, httpWorld bool, admin *sql.DB, conn *db.Conn) tasktest.Fixture {
	t.Helper()
	svc := &policyCommands{call: func(ctx context.Context, id uuid.UUID, verb string, assignee uuid.UUID, resolution string) (out *contracts.Task, err error) {
		err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			service := task.NewServiceWithPolicy(policy)
			if verb == "resolve" {
				out, err = service.Resolve(ctx, tx, id, resolution)
			} else {
				out, err = service.Assign(ctx, tx, id, assignee)
			}
			return err
		})
		return out, err
	}}
	if httpWorld {
		svc.call = func(ctx context.Context, id uuid.UUID, verb string, assignee uuid.UUID, resolution string) (*contracts.Task, error) {
			return policyHTTP(t, conn, policy, ctx, id, verb, assignee, resolution, nil)
		}
	}
	return tasktest.Fixture{Ctx: ctx, Service: svc,
		Seed: func(row *contracts.Task) uuid.UUID {
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return crud.Create(ctx, tx, row) }); err != nil {
				t.Fatal(err)
			}
			return row.ID
		},
		Task: func(id uuid.UUID) (row contracts.Task, err error) {
			err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				got, err := crud.Get[*contracts.Task](tx, id)
				if err == nil {
					row = *got
				}
				return err
			})
			return row, err
		},
		Published: func() []string {
			rows, err := admin.QueryContext(t.Context(), "SELECT name FROM platformkit_outbox WHERE name IN ('task.assigned', 'task.resolved') ORDER BY created_at, id")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var names []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				names = append(names, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return names
		},
	}
}

type policyCommands struct {
	contracts.Service
	call func(context.Context, uuid.UUID, string, uuid.UUID, string) (*contracts.Task, error)
}

func (s *policyCommands) Assign(ctx context.Context, _ db.Tx[db.Tenant], id, who uuid.UUID) (*contracts.Task, error) {
	return s.call(ctx, id, "assign", who, "")
}

func (s *policyCommands) Resolve(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID, text string) (*contracts.Task, error) {
	return s.call(ctx, id, "resolve", uuid.Nil, text)
}

type policyTenant struct{ tenancy.Tenant }

func (p policyTenant) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return p.Tenant, nil
}

func policyHTTP(t *testing.T, conn *db.Conn, policy tenancy.Policy, ctx context.Context, id uuid.UUID, verb string, assignee uuid.UUID, resolution string, before func(context.Context, db.Tx[db.Tenant]) error) (*contracts.Task, error) {
	t.Helper()
	tenant, _ := tenancy.FromContext(ctx)
	principal, _ := tenancy.PrincipalFrom(ctx)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: policyTenant{tenant}, Conn: conn, Authorize: caller{},
		Authenticate: func(ctx context.Context, tx db.Tx[db.Tenant], _ *http.Request) (tenancy.Principal, bool, error) {
			if before != nil {
				if err := before(ctx, tx); err != nil {
					return tenancy.Principal{}, false, err
				}
			}
			return principal, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	task.New(task.Deps{Policy: policy}).Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"resolution": resolution}
	if verb == "assign" {
		body = map[string]string{"assigneeId": assignee.String()}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+path+"/"+id.String()+"/"+verb, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		var out contracts.Task
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return &out, nil
	}
	if strings.Contains(w.Body.String(), "private provider") || strings.Contains(w.Body.String(), `"assigneeId"`) || strings.Contains(w.Body.String(), `"revision"`) {
		t.Fatalf("refusal leaked task/provider: %s", w.Body.String())
	}
	if (w.Code == 403 && !strings.Contains(w.Body.String(), "POLICY_DENIED")) || (w.Code == 503 && !strings.Contains(w.Body.String(), "POLICY_UNAVAILABLE")) {
		t.Fatalf("policy refusal lost its code: %s", w.Body.String())
	}
	for code, refusal := range map[int]error{403: tenancy.ErrPolicyDenied, 404: crud.ErrNotFound, 409: crud.ErrConflict, 422: crud.ErrInvalid, 503: tenancy.ErrPolicyUnavailable} {
		if w.Code == code {
			return nil, refusal
		}
	}
	return nil, fmt.Errorf("HTTP %d: %s", w.Code, w.Body.String())
}
