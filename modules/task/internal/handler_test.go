package internal_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

const host = "acme.test"

const path = "/api/v1/task/tasks"

// caller is the three answers httpx.New needs. The tenant module and the auth
// module give the real ones in E3.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

// mounted is the module's routes behind the real API: the tenant resolved from
// the host, the transaction opened on the first query, the permission checked,
// and the response held until the commit.
func mounted(t *testing.T) (*httpx.API, chi.Router, *db.Conn) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(*http.Request) (httpx.Principal, bool) {
			return httpx.Principal{UserID: uuid.New(), TenantID: acme.ID}, true
		},
		Log: slog.New(slog.DiscardHandler),
	})
	internal.RegisterRoutes(api, internal.NewService(), path)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, conn
}

func call(t *testing.T, r http.Handler, method, at, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// seed writes one task straight into the tenant, so the lifecycle routes have
// something to act on without depending on the CRUD routes the Spec mounts.
func seed(t *testing.T, conn *db.Conn, title string) uuid.UUID {
	t.Helper()
	task := &contracts.Task{Title: title}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, task)
	})
	if err != nil {
		t.Fatalf("seed %q: %v", title, err)
	}
	return task.ID
}

// TestTheThreeCommandsAnswer walks every status the lifecycle routes produce,
// through the whole middleware chain, against a real Postgres.
func TestTheThreeCommandsAnswer(t *testing.T) {
	_, router, conn := mounted(t)
	who := uuid.NewString()

	id := seed(t, conn, "chiller")
	code, body := call(t, router, http.MethodPost, path+"/"+id.String()+"/assign", `{"assigneeId":"`+who+`"}`)
	if code != http.StatusOK || !strings.Contains(body, `"acknowledged"`) {
		t.Fatalf("assign = %d %s, want 200 and an acknowledged task", code, body)
	}
	// The retry a browser sends is the same answer and no second event.
	if code, body = call(t, router, http.MethodPost, path+"/"+id.String()+"/assign", `{"assigneeId":"`+who+`"}`); code != http.StatusOK {
		t.Errorf("the second assign = %d %s", code, body)
	}

	if code, body = call(t, router, http.MethodPost, path+"/"+id.String()+"/resolve", `{"resolution":"swapped the valve"}`); code != http.StatusOK ||
		!strings.Contains(body, `"resolved"`) {
		t.Errorf("resolve = %d %s, want 200 and a resolved task", code, body)
	}
	if code, body = call(t, router, http.MethodPost, path+"/"+id.String()+"/check-sla", ""); code != http.StatusOK {
		t.Errorf("check-sla = %d %s", code, body)
	}
}

// TestTheCommandsRefuseWithTheRightStatus: the three failures a caller can act
// on, each as the code that says which one it was. Every refusal ends the
// request's transaction, so each case gets a task of its own.
func TestTheCommandsRefuseWithTheRightStatus(t *testing.T) {
	_, router, conn := mounted(t)
	resolved := func() uuid.UUID {
		id := seed(t, conn, "already done "+uuid.NewString())
		if code, body := call(t, router, http.MethodPost, path+"/"+id.String()+"/resolve", `{"resolution":"first"}`); code != http.StatusOK {
			t.Fatalf("resolve = %d %s", code, body)
		}
		return id
	}

	for _, tt := range []struct {
		what string
		at   string
		body string
		want int
	}{
		{"a task nobody has", path + "/" + uuid.NewString() + "/assign", `{"assigneeId":"` + uuid.NewString() + `"}`, http.StatusNotFound},
		{"no assignee", path + "/" + seed(t, conn, "unassignable").String() + "/assign",
			`{"assigneeId":"00000000-0000-0000-0000-000000000000"}`, http.StatusUnprocessableEntity},
		{"assigning a resolved task", path + "/" + resolved().String() + "/assign",
			`{"assigneeId":"` + uuid.NewString() + `"}`, http.StatusConflict},
		{"a second, different resolution", path + "/" + resolved().String() + "/resolve",
			`{"resolution":"second"}`, http.StatusConflict},
	} {
		t.Run(tt.what, func(t *testing.T) {
			code, body := call(t, router, http.MethodPost, tt.at, tt.body)
			if code != tt.want {
				t.Errorf("%s = %d %s, want %d", tt.what, code, body, tt.want)
			}
			if !strings.Contains(body, `"status":`) {
				t.Errorf("%s answered %s, which is not a problem document", tt.what, body)
			}
		})
	}
}

// TestEveryRouteDeclaresItsPermission reads the recording kit/app's boot gate
// reads. A route that forgot to say who may call it cannot exist — Register
// takes the declaration as a parameter — so what is checked here is that the
// three commands ask for task:update and not, say, task:read.
func TestEveryRouteDeclaresItsPermission(t *testing.T) {
	api, _, _ := mounted(t)
	want := `{"kind":"permission","permission":"` + contracts.PermissionTaskUpdate + `"}`
	seen := map[string]bool{}
	for _, op := range api.Recorded() {
		if !strings.HasPrefix(op.OperationID, "task-task-") {
			continue
		}
		seen[op.OperationID] = true
		got, err := json.Marshal(op.Extensions[httpx.AuthExtension])
		if err != nil || string(got) != want {
			t.Errorf("%s declares %s, want %s (%v)", op.OperationID, got, want, err)
		}
	}
	for _, id := range []string{"task-task-assign", "task-task-resolve", "task-task-check-sla"} {
		if !seen[id] {
			t.Errorf("%s was never registered", id)
		}
	}
	// And each says which event it will publish, which is the same recording
	// and the same gate.
	if got := strings.Join(api.Events(), " "); got != "task.assigned task.resolved task.sla_breached" {
		t.Errorf("the commands publish %q", got)
	}
}
