package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// sweepLimit bounds one tenant's pass. A tenant with more overdue tasks than
// this has a bigger problem than the sweep, and the next tick takes the rest;
// an unbounded pass would hold one transaction open across an incident.
const sweepLimit = 500

// SLASweep is the module's periodic work: the one thing an outbox cannot
// express, because a deadline passing is not something that happened to anybody
// (docs/adr/0004). Every tick, in every tenant, every task whose deadline has
// gone by unresolved is handed to CheckSLA.
//
// One instance in the cluster runs it per tick — kit/jobs takes an advisory
// lock named after the job — and each tenant gets its own transaction.
func SLASweep(tenants jobs.TenantLister, svc contracts.Service, every time.Duration) jobs.Job {
	return jobs.Job{
		Name:  "task-sla-sweep",
		Every: every,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.ForEachTenant(ctx, conn, tenants, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return sweep(ctx, tx, svc)
			})
		},
	}
}

// sweep is one tenant's pass. The overdue set is a range comparison, which
// kit/crud's equality filters cannot express, so this is a query rather than a
// crud.List; it reads only the ids, because each CheckSLA reads the row it is
// about to write and carrying whole rows here would read them twice.
func sweep(ctx context.Context, tx db.Tx[db.Tenant], svc contracts.Service) error {
	var overdue []uuid.UUID
	err := tx.DB().Model(&contracts.Task{}).
		Where("sla_breached = false AND resolved_at IS NULL AND sla_deadline < now() AND deleted_at IS NULL").
		Order("sla_deadline").Limit(sweepLimit).Pluck("id", &overdue).Error
	if err != nil {
		return fmt.Errorf("task: list the overdue tasks: %w", err)
	}
	for _, id := range overdue {
		if _, err := svc.CheckSLA(ctx, tx, id); err != nil {
			return fmt.Errorf("task: check the SLA of %s: %w", id, err)
		}
	}
	return nil
}

// BreachOnArrival is the create route's hook: a task whose deadline has already
// passed — an integration replaying yesterday's alarms, a form filled in late —
// is breached now rather than within a minute of the next sweep.
//
// It runs inside the request's transaction, after the row and its created
// event, so an error here rolls the create back. That is the property that
// makes a hook worth having over a subscriber, which could only ever run after
// the create had already committed.
func BreachOnArrival(svc contracts.Service) func(context.Context, db.Tx[db.Tenant], *contracts.Task) error {
	return func(ctx context.Context, tx db.Tx[db.Tenant], task *contracts.Task) error {
		if !task.IsOverdue(time.Now()) {
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
