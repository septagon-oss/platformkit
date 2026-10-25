package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type precisionTask struct {
	Task
	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`
	Control     int32  `json:"control"`
	Revision    int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false"`
}

type precisionCaller struct {
	denied  atomic.Bool
	foreign tenancy.Tenant
}

func (c *precisionCaller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return !c.denied.Load(), nil
}
func (c *precisionCaller) ByHost(ctx context.Context, tx db.Tx[db.System], name string) (tenancy.Tenant, error) {
	if name == "foreign.test" {
		return c.foreign, nil
	}
	return caller{}.ByHost(ctx, tx, name)
}

type precisionWorld struct {
	router       http.Handler
	actor        *precisionCaller
	read         func(uuid.UUID) *precisionTask
	events       func() []string
	seedRevision func(uuid.UUID, int64)
}

const precisionPath = "/api/v1/precision/task"

func precisionFixture(t *testing.T, fake bool) precisionWorld {
	t.Helper()
	actor := &precisionCaller{foreign: tenancy.Tenant{ID: uuid.New(), Slug: "foreign"}}
	spec := rest.Spec[*precisionTask]{Module: "precision", Entity: "task", Path: "/task", Read: "task:read", Write: "task:write"}
	if fake {
		spec.Operations = []httpx.CRUD{httpx.CRUDDelete}
	}
	api, router, admin := mountAs(t, spec, actor)
	w := precisionWorld{router: router, actor: actor}
	if fake {
		return precisionFake(t, api, w)
	}
	_, err := admin.ExecContext(t.Context(), `ALTER TABLE rest_tasks ADD COLUMN amount_cents bigint NOT NULL DEFAULT 0, ADD COLUMN currency text NOT NULL DEFAULT '', ADD COLUMN control integer NOT NULL DEFAULT 0, ADD COLUMN revision bigint NOT NULL DEFAULT 1`)
	if err != nil {
		t.Fatal(err)
	}
	w.read = func(id uuid.UUID) *precisionTask {
		var row precisionTask
		if err := admin.QueryRowContext(t.Context(), `SELECT title, amount_cents, currency, control, priority, revision FROM rest_tasks WHERE id=$1`, id).Scan(&row.Title, &row.AmountCents, &row.Currency, &row.Control, &row.Priority, &row.Revision); err != nil {
			t.Fatal(err)
		}
		return &row
	}
	w.events = func() []string {
		var data []byte
		if err := admin.QueryRowContext(t.Context(), `SELECT COALESCE(json_agg(name ORDER BY created_at,id), '[]') FROM platformkit_outbox WHERE name LIKE 'precision.%'`).Scan(&data); err != nil {
			t.Fatal(err)
		}
		var names []string
		if err := json.Unmarshal(data, &names); err != nil {
			t.Fatal(err)
		}
		return names
	}
	w.seedRevision = func(id uuid.UUID, n int64) {
		if _, err := admin.ExecContext(t.Context(), `UPDATE rest_tasks SET revision=$1 WHERE id=$2`, n, id); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

type precisionReply struct {
	ETag string `header:"ETag"`
	Body *precisionTask
}
type precisionRead struct {
	ID uuid.UUID `path:"id"`
}
type precisionPatch struct {
	ID      uuid.UUID `path:"id"`
	IfMatch string    `header:"If-Match"`
	Body    map[string]any
}

func precisionFake(t *testing.T, api *httpx.API, w precisionWorld) precisionWorld {
	t.Helper()
	f := porttest.NewFake(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), []string{"precision.task.created", "precision.task.updated"})
	f.Grants.Grant(principal, "task:read", "task:write")
	store := porttest.NewStore(func(row *precisionTask) uuid.UUID { return row.ID })
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), principal)
	w.read = func(id uuid.UUID) *precisionTask {
		row, err := store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	w.events = f.Events.Names
	w.seedRevision = func(id uuid.UUID, n int64) { row := w.read(id); row.Revision = n; store.Put(ctx, row) }
	router := api.Surfaces("precision").App
	httpx.Register(router, huma.Operation{OperationID: "precision-create", Method: "POST", Path: "/task", DefaultStatus: 201}, httpx.Permission("task:write"),
		func(ctx context.Context, in *struct{ Body *precisionTask }) (*rest.Item[*precisionTask], error) {
			if !f.Grants.Holds(ctx, "task:write") {
				return nil, rest.Fault(tenancy.ErrPolicyDenied)
			}
			row := in.Body
			if err := row.Validate(ctx); err != nil {
				return nil, rest.Fault(fmt.Errorf("%w: %s", crud.ErrInvalid, err))
			}
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, db.ErrNoTenant
			}
			row.ID, row.TenantID, row.Revision = uuid.New(), db.TenantOf(tx).ID, 1
			row.CreatedAt, row.UpdatedAt = f.Clock.Now(), f.Clock.Now()
			store.Put(ctx, row)
			if err := f.Events.Emit("precision.task.created"); err != nil {
				return nil, err
			}
			return &rest.Item[*precisionTask]{Body: row}, nil
		})
	httpx.Register(router, huma.Operation{OperationID: "precision-read", Method: "GET", Path: "/task/{id}"}, httpx.Permission("task:read"),
		func(ctx context.Context, in *precisionRead) (*precisionReply, error) {
			row, err := store.Get(ctx, in.ID)
			if err != nil {
				return nil, rest.Fault(err)
			}
			return precisionAnswer(row), nil
		})
	httpx.Register(router, huma.Operation{OperationID: "precision-list", Method: "GET", Path: "/task"}, httpx.Permission("task:read"),
		func(ctx context.Context, _ *struct{}) (*rest.Page[*precisionTask], error) {
			out := new(rest.Page[*precisionTask])
			out.Body.Items = store.All(ctx)
			out.Body.Total = int64(len(out.Body.Items))
			out.Body.Limit = 50
			return out, nil
		})
	mountPrecisionFakePatch(router, f, store)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return w
}

func precisionAnswer(row *precisionTask) *precisionReply {
	return &precisionReply{Body: row, ETag: strconv.Quote(strconv.FormatInt(row.Revision, 10))}
}

func mountPrecisionFakePatch(router *httpx.Router, f *porttest.Fake, store *porttest.Store[*precisionTask]) {
	httpx.Register(router, huma.Operation{OperationID: "precision-patch", Method: "PATCH", Path: "/task/{id}"}, httpx.Permission("task:write"),
		func(ctx context.Context, in *precisionPatch) (*precisionReply, error) {
			row, err := porttest.Do(ctx, f, store, porttest.Command[*precisionTask]{Permission: "task:write", Denied: tenancy.ErrPolicyDenied, Row: in.ID,
				Decide: func(row *precisionTask) error {
					if err := rest.CheckIntegerFixtureRevision(in.IfMatch, row.Revision); err != nil {
						return err
					}
					if err := rest.MergeIntegerFixture(row, crud.Fields[*precisionTask](), in.Body); err != nil {
						return err
					}
					if err := row.Validate(ctx); err != nil {
						return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
					}
					return nil
				}, Apply: func(row **precisionTask) []string { (*row).Revision++; return []string{"precision.task.updated"} }})
			if err != nil {
				return nil, rest.Fault(err)
			}
			return precisionAnswer(row), nil
		})
}
