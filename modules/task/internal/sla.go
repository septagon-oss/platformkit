package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// sweepLimit bounds one tenant's pass. A tenant with more overdue tasks than
// this has a bigger problem than the sweep, and the next tick takes the rest.
const sweepLimit = 500

// SLASweep is the module's periodic work: the one thing an outbox cannot
// express, because a deadline passing is not something that happened to anybody
// (docs/adr/0004). Every tick, in every tenant, every task whose deadline has
// gone by unresolved is handed to CheckSLA.
//
// One instance in the cluster runs it per tick — kit/jobs takes an advisory
// lock named after the job — and each task gets its own transaction.
func SLASweep(tenants jobs.TenantLister, svc contracts.Service, every time.Duration) jobs.Job {
	return jobs.Job{
		Name:  "task-sla-sweep",
		Every: every,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return sweep(ctx, conn, tenants, svc)
		},
	}
}

// overdue is one tenant's set of tasks past their deadline.
type overdue struct {
	tenant tenancy.Tenant
	ids    []uuid.UUID
}

// sweep reads every tenant's overdue set and then breaches each task in a
// transaction of its own. A row an older writer left in a shape Validate
// refuses — a priority that is no longer a priority — then fails its own
// CheckSLA and nothing else, where one transaction per tenant meant that row
// rolled every breach beside it back, on every tick, forever. The failures come
// back joined, each logged with its task id, because "the sweep failed" with no
// row to look at is not something anybody can act on.
//
// The two passes cannot be one: db.Run joins an enclosing tenant transaction
// rather than opening a second, so a per-task transaction started inside the
// listing transaction would silently be the listing transaction.
func sweep(ctx context.Context, conn *db.Conn, tenants jobs.TenantLister, svc contracts.Service) error {
	var work []overdue
	failed := []error{jobs.ForEachTenant(ctx, conn, tenants, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		// A range comparison, which kit/crud's equality filters cannot express,
		// so this is a query and not a crud.List; it reads only the ids,
		// because each CheckSLA reads the row it is about to write.
		var ids []uuid.UUID
		err := tx.DB().Model(&contracts.Task{}).
			Where("sla_breached = false AND resolved_at IS NULL AND sla_deadline < now() AND deleted_at IS NULL").
			Order("sla_deadline").Limit(sweepLimit).Pluck("id", &ids).Error
		if err != nil {
			return fmt.Errorf("task: list the overdue tasks: %w", err)
		}
		work = append(work, overdue{tenant: db.TenantOf(tx), ids: ids})
		return nil
	})}

	for _, w := range work {
		tenant := tenancy.WithTenant(ctx, w.tenant)
		for _, id := range w.ids {
			err := db.Run(tenant, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.CheckSLA(ctx, tx, id)
				return err
			})
			if err != nil {
				slog.ErrorContext(ctx, "task: could not record a breach; the sweep continues",
					"tenant", w.tenant.Slug, "task", id, "error", err)
				failed = append(failed, fmt.Errorf("task: check the SLA of %s: %w", id, err))
			}
		}
	}
	return errors.Join(failed...)
}

// BreachOnArrival is the create route's hook: a task whose deadline has already
// passed — an integration replaying yesterday's alarms, a form filled in late —
// is breached now rather than within a minute of the next sweep.
//
// It runs inside the request's transaction, after the row and its created
// event, so an error here rolls the create back. That is the property that
// makes a hook worth having over a subscriber, which could only ever run after
// the create had already committed. The event it publishes is declared by the
// Spec's HookEvents, because nothing reads a hook to find out what it emits.
func BreachOnArrival(svc contracts.Service) func(context.Context, db.Tx[db.Tenant], *contracts.Task) error {
	return func(ctx context.Context, tx db.Tx[db.Tenant], task *contracts.Task) error {
		if !task.IsOverdue(db.Now()) {
			return nil
		}
		breached, err := svc.CheckSLA(ctx, tx, task.ID)
		if err != nil {
			return err
		}
		*task = *breached // so the 201 shows the state the row is actually in
		return nil
	}
}
