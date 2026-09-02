package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// renewCron is a quarter past two in the morning, every day. A period ends on a
// date and not at an hour, so a day of granularity is all a renewal has, and
// the small hours are when a payment processor is least busy.
const renewCron = "15 2 * * *"

// Renew is the module's periodic work: the one thing an outbox cannot express,
// because a period running out is not something that happened to anybody
// (docs/adr/0004). One instance in the cluster runs it per tick, kit/jobs
// taking an advisory lock named after the job.
func Renew(tenants jobs.TenantLister, svc contracts.Service, payments contracts.PaymentProvider, every time.Duration) jobs.Job {
	job := jobs.Job{
		Name: "billing-renew",
		Cron: renewCron,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return renew(ctx, conn, tenants, svc, payments)
		},
	}
	if every > 0 { // a test, which cannot wait until two in the morning
		job.Cron, job.Every = "", every
	}
	return job
}

// renew advances one tenant's subscription, in two transactions with a charge
// between them, and the shape is the whole point: taking money happens with no
// transaction open, so a provider that hangs costs this tenant its renewal for
// a night and holds no database transaction while it does.
//
// jobs.PerTenant hands over a context carrying the tenant and no open
// transaction, so each db.Run below is its own, and one tenant's failure is
// logged where it happened and does not stop the tenants after it.
func renew(ctx context.Context, conn *db.Conn, tenants jobs.TenantLister,
	svc contracts.Service, payments contracts.PaymentProvider,
) error {
	return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
		var charge *contracts.Charge
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, c, err := svc.Renew(ctx, tx)
			charge = c
			return err
		})
		switch {
		case errors.Is(err, crud.ErrNotFound):
			// This tenant has never subscribed, which is most of them on the
			// day an installation turns billing on.
			return nil
		case err != nil:
			return fmt.Errorf("billing: renew %s: %w", t.Slug, err)
		case charge == nil:
			return nil
		}
		receipt, err := payments.Charge(ctx, *charge)
		if err != nil {
			return fmt.Errorf("billing: charge %s for %s: %w", charge.PlanCode, t.Slug, err)
		}
		err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Settle(ctx, tx, *charge, receipt)
			return err
		})
		if err != nil {
			return fmt.Errorf("billing: settle %s for %s: %w", receipt.Reference, t.Slug, err)
		}
		return nil
	})
}

// Manual is the PaymentProvider for an installation that takes money somewhere
// else: it records what is owed, in the log, and settles nothing.
//
// It is not a stub. An unsettled receipt is a real answer — Settle marks the
// subscription past due and publishes billing.past_due — so a deployment with
// no payment processor still runs the whole lifecycle and still says, once per
// customer, that somebody owes money. A provider that lied and said Settled
// would be a deployment quietly giving everything away.
type Manual struct{}

// NewManual returns the provider that moves no money. main wires it, so the
// choice is visible in the file that composes the application.
func NewManual() *Manual { return &Manual{} }

var _ contracts.PaymentProvider = (*Manual)(nil)

// Charge records what is due and takes nothing. The reference is derived rather
// than generated, so asking twice for one period is one reference: a renewal
// that runs again after a crash must not look like a second debt.
func (Manual) Charge(ctx context.Context, c contracts.Charge) (contracts.Receipt, error) {
	slog.InfoContext(ctx, "billing: a charge is due and this installation takes no payments",
		"subscription", c.Subscription, "plan", c.PlanCode,
		"amount_cents", c.AmountCents, "currency", c.Currency, "period_end", c.PeriodEnd)
	return contracts.Receipt{
		Reference: "manual:" + c.Subscription.String() + ":" + c.PeriodEnd.UTC().Format(time.RFC3339),
		At:        db.Now(),
	}, nil
}
