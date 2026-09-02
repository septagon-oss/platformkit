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

// sweep breaches every overdue task, each in a transaction of its own.
//
// jobs.PerTenant hands over a context that already carries the tenant and no
// open transaction, so every db.Run below is its own: the listing is one, and
// each task is another. That is what makes a row an older writer left in a
// shape Validate refuses — a priority that is no longer a priority — fail its
// own CheckSLA and nothing else, where one transaction per tenant meant that
// row rolled every breach beside it back, on every tick, forever. The failures
// come back joined, each logged with its task id, because "the sweep failed"
// with no row to look at is not something anybody can act on.
func sweep(ctx context.Context, conn *db.Conn, tenants jobs.TenantLister, svc contracts.Service) error {
	return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, tenant tenancy.Tenant) error {
		var ids []uuid.UUID
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			// A range comparison, which kit/crud's equality filters cannot
			// express, so this is a query and not a crud.List; it reads only
			// the ids, because each CheckSLA reads the row it is about to write.
			return tx.DB().Model(&contracts.Task{}).
				Where("sla_breached = false AND resolved_at IS NULL AND sla_deadline < now() AND deleted_at IS NULL").
				Order("sla_deadline").Limit(sweepLimit).Pluck("id", &ids).Error
		})
		if err != nil {
			return fmt.Errorf("task: list the overdue tasks: %w", err)
		}
		var failed []error
		for _, id := range ids {
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.CheckSLA(ctx, tx, id)
				return err
			})
			if err != nil {
				slog.ErrorContext(ctx, "task: could not record a breach; the sweep continues",
					"tenant", tenant.Slug, "task", id, "error", err)
				failed = append(failed, fmt.Errorf("task: check the SLA of %s: %w", id, err))
			}
		}
		return errors.Join(failed...)
	})
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
