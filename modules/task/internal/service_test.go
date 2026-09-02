package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction. Two implementations,
// one specification: that is what the interface is for.
//
// Each case gets its own schema, so no case sees another's rows, and the
// harness gives it the transaction rather than opening one per command —
// which is exactly how a request handler calls these.
func TestServiceConforms(t *testing.T) {
	tasktest.RunService(t, func(t *testing.T, run func(tasktest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService()
		// One transaction per case, rolled back on the way out: a test keeps
		// nothing, and the commands are called exactly as a request handler
		// calls them — inside a transaction somebody else opened.
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(tasktest.Fixture{Ctx: ctx, Tx: tx, Service: svc, Seed: func(task *contracts.Task) uuid.UUID {
				if err := crud.Create(ctx, tx, task); err != nil {
					t.Fatalf("seed a task: %v", err)
				}
				return task.ID
			}})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// errRollback ends a conformance case's transaction without committing it.
var errRollback = errors.New("rolled back on purpose")

// TestTheCommandsPublishInTheCallersTransaction is what the conformance suite
// cannot see, because the fake has no outbox: a state change and the event that
// describes it are one row each in one transaction, so neither can outlive the
// other. It also pins the idempotent paths as silent — a retry that publishes
// is a workload dashboard counting one assignment twice.
func TestTheCommandsPublishInTheCallersTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()
	who := uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		overdue := &contracts.Task{Title: "chiller", Priority: contracts.PriorityCritical}
		deadline := time.Now().Add(-time.Hour)
		overdue.SLADeadline = &deadline
		if err := crud.Create(ctx, tx, overdue); err != nil {
			return err
		}
		// Each command twice: the second is the retry, and says nothing.
		for _, command := range []func() error{
			func() error { _, err := svc.Assign(ctx, tx, overdue.ID, who); return err },
			func() error { _, err := svc.CheckSLA(ctx, tx, overdue.ID); return err },
			func() error { _, err := svc.Resolve(ctx, tx, overdue.ID, "swapped the valve"); return err },
		} {
			for range 2 {
				if err := command(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the commands: %v", err)
	}

	// One event per command, whichever order the outbox rows ended up in: the
	// claim here is that each command published exactly once and each retry
	// published nothing.
	for _, name := range []string{contracts.EventAssigned, contracts.EventSLABreached, contracts.EventResolved} {
		var count int
		err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`, name, acme.ID).Scan(&count)
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 1 {
			t.Errorf("%s was published %d times, want once", name, count)
		}
	}
	var total int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&total); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if total != 3 {
		t.Errorf("the outbox holds %d events, want the three the commands published", total)
	}
}

// TestARolledBackCommandLeavesNothing: the other half of the same claim. A
// transaction that does not commit leaves neither the assignment nor its event,
// so a subscriber can never be told about a change that did not happen.
func TestARolledBackCommandLeavesNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		task := &contracts.Task{Title: "chiller"}
		if err := crud.Create(ctx, tx, task); err != nil {
			return err
		}
		id = task.ID
		return nil
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Assign(ctx, tx, id, uuid.New()); err != nil {
			return err
		}
		return context.Canceled // whatever went wrong after the command
	})

	var status string
	var events int
	if err := admin.QueryRowContext(t.Context(), `SELECT status FROM tasks WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if status != contracts.StatusOpen || events != 0 {
		t.Errorf("after the rollback the task is %q and there are %d events; want open and none", status, events)
	}
}

// TestTheSweepBreachesTheOverdueAndNothingElse drives the periodic job the way
// the worker does, across two tenants, and checks both what it changed and what
// it left alone — including the second pass, which is the ordinary case: the
// sweep runs every minute forever and must publish once per task.
func TestTheSweepBreachesTheOverdueAndNothingElse(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	svc := internal.NewService()

	titles := map[string]*time.Time{}
	hour := func(d time.Duration) *time.Time { at := time.Now().Add(d); return &at }
	titles["overdue"] = hour(-time.Hour)
	titles["due later"] = hour(time.Hour)
	titles["no deadline"] = nil

	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for title, deadline := range titles {
				task := &contracts.Task{Title: title, SLADeadline: deadline}
				if err := crud.Create(ctx, tx, task); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seed %s: %v", tenant.Slug, err)
		}
	}

	sweep := internal.SLASweep(lister{acme, globex}, svc, time.Minute)
	for pass := range 2 {
		if err := sweep.Run(t.Context(), conn); err != nil {
			t.Fatalf("sweep pass %d: %v", pass, err)
		}
	}

	var breached, events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM tasks WHERE sla_breached`).Scan(&breached); err != nil {
		t.Fatalf("count the breaches: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1`, contracts.EventSLABreached).Scan(&events); err != nil {
		t.Fatalf("count the events: %v", err)
	}
	if breached != 2 || events != 2 {
		t.Errorf("%d breached tasks and %d events; want one per tenant, once", breached, events)
	}
	if sweep.Name != "task-sla-sweep" {
		t.Errorf("the job is called %q; the name is its advisory lock and has to be stable", sweep.Name)
	}
}

// lister is what the tenant module implements in E3, and what
// apps/platformkit/dev.go implements until then.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }
