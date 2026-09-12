package task_test

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// A product shares the lifecycle service with the task module. Exercise that
// composition through committed database state, HTTP and the manifest's job.
func TestModuleUsesComposedService(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := &observedService{Service: task.NewService()}
	manifest := task.Module(task.Deps{Service: svc, Tenants: activeTenants{acme}})
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	manifest.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}

	code, body := call(t, router, http.MethodPost, path, `{"title":"Vehicle requires inspection","slaDeadline":"2020-01-01T00:00:00Z"}`)
	if code != http.StatusCreated {
		t.Fatalf("create overdue task: %d %s", code, body)
	}
	incident := uuid.MustParse(id(t, body))
	assignee := uuid.New()
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Assign(ctx, tx, incident, assignee)
		return err
	}); err != nil {
		t.Fatalf("product assignment: %v", err)
	}
	for _, command := range []struct{ name, body string }{
		{"assign", `{"assigneeId":"` + assignee.String() + `"}`},
		{"resolve", `{"resolution":"Inspection complete"}`},
		{"check-sla", `{}`},
	} {
		code, body := call(t, router, http.MethodPost, path+"/"+incident.String()+"/"+command.name, command.body)
		if code != http.StatusOK {
			t.Fatalf("HTTP %s: %d %s", command.name, code, body)
		}
	}

	// A task raised outside HTTP must reach the same service on the next sweep.
	queued := &contracts.Task{Title: "Replacement needed", SLADeadline: new(time.Now().Add(-time.Hour))}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, queued)
	}); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Jobs) != 1 {
		t.Fatalf("task jobs = %d, want the SLA sweep", len(manifest.Jobs))
	}
	for range 2 {
		if err := manifest.Jobs[0].Run(t.Context(), conn); err != nil {
			t.Fatalf("SLA sweep: %v", err)
		}
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		resolved, err := crud.Get[*contracts.Task](tx, incident)
		if err != nil {
			return err
		}
		if resolved.Status != contracts.StatusResolved || resolved.Resolution != "Inspection complete" ||
			resolved.ResolvedAt == nil || resolved.AssigneeID == nil || *resolved.AssigneeID != assignee || !resolved.SLABreached {
			t.Errorf("committed resolved task = %+v", resolved)
		}
		waiting, err := crud.Get[*contracts.Task](tx, queued.ID)
		if err == nil && (waiting.Status != contracts.StatusOpen || !waiting.SLABreached) {
			t.Errorf("committed swept task = %+v", waiting)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	want := []serviceCall{
		{"check-sla", incident}, {"assign", incident}, {"assign", incident},
		{"resolve", incident}, {"check-sla", incident}, {"check-sla", queued.ID},
	}
	if !slices.Equal(svc.calls, want) {
		t.Errorf("composed service calls = %v, want %v", svc.calls, want)
	}
	rows, err := admin.QueryContext(t.Context(), `SELECT name, count(*) FROM platformkit_outbox GROUP BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	events := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			t.Fatal(err)
		}
		events[name] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{
		contracts.EventCreated: 1, contracts.EventAssigned: 1,
		contracts.EventResolved: 1, contracts.EventSLABreached: 2,
	}; !maps.Equal(events, want) {
		t.Errorf("committed events = %v, want %v", events, want)
	}
}

type serviceCall struct {
	command string
	id      uuid.UUID
}

type observedService struct {
	contracts.Service
	calls []serviceCall
}

func (s *observedService) Assign(ctx context.Context, tx db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	s.calls = append(s.calls, serviceCall{"assign", id})
	return s.Service.Assign(ctx, tx, id, assignee)
}

func (s *observedService) Resolve(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, resolution string) (*contracts.Task, error) {
	s.calls = append(s.calls, serviceCall{"resolve", id})
	return s.Service.Resolve(ctx, tx, id, resolution)
}

func (s *observedService) CheckSLA(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	s.calls = append(s.calls, serviceCall{"check-sla", id})
	return s.Service.CheckSLA(ctx, tx, id)
}

type activeTenants []tenancy.Tenant

func (t activeTenants) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return t, nil
}
