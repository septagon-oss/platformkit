package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
	"github.com/septagon-oss/platformkit/modules/billing/contracts/billingtest"
	"github.com/septagon-oss/platformkit/modules/billing/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// errRollback ends a conformance case's transaction without committing it.
var errRollback = errors.New("rolled back on purpose")

// outbox is the table kit/events writes to, named here because the conformance
// fixture reads what the commands published and only kit/events writes it.
const outbox = "platformkit_outbox"

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction. Two implementations,
// one specification: that is what the interface is for.
func TestServiceConforms(t *testing.T) {
	billingtest.RunService(t, func(t *testing.T, run func(billingtest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(billingtest.Fixture{Ctx: ctx, Tx: tx, Service: svc,
				Plan: func(plan *contracts.Plan) uuid.UUID {
					if err := crud.Create(ctx, tx, plan); err != nil {
						t.Fatalf("seed a plan: %v", err)
					}
					return plan.ID
				},
				Expire: func() {
					// Straight at the columns, because nothing in this module
					// moves a period backwards: this is the clock, not a command.
					err := tx.DB().Exec(
						`UPDATE billing_subscriptions SET current_period_start = now() - interval '32 days',
							current_period_end = now() - interval '1 day'`).Error
					if err != nil {
						t.Fatalf("expire the period: %v", err)
					}
				},
				Published: func() []string {
					var names []string
					err := tx.DB().Raw(`SELECT name FROM `+outbox+` WHERE tenant_id = ? ORDER BY created_at, id`, acme.ID).
						Scan(&names).Error
					if err != nil {
						t.Fatalf("read the outbox: %v", err)
					}
					return names
				}})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// TestTheCommandsPublishInTheCallersTransaction is what the conformance suite
// cannot see, because the fake has no outbox: a state change and the event that
// describes it are one row each in one transaction, so neither can outlive the
// other. It also pins the idempotent paths as silent.
func TestTheCommandsPublishInTheCallersTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		plan := &contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Active: true}
		if err := crud.Create(ctx, tx, plan); err != nil {
			return err
		}
		// Each command twice: the second is the retry, and says nothing.
		for range 2 {
			if _, err := svc.Subscribe(ctx, tx, plan.ID); err != nil {
				return err
			}
		}
		if err := tx.DB().Exec(
			`UPDATE billing_subscriptions SET current_period_start = now() - interval '32 days',
				current_period_end = now() - interval '1 day'`).Error; err != nil {
			return err
		}
		var charge *contracts.Charge
		var err error
		if _, charge, err = svc.Renew(ctx, tx); err != nil {
			return err
		}
		if charge == nil {
			t.Fatal("a paid plan whose period has run out owes something")
		}
		unpaid := contracts.Receipt{Reference: "manual:1"}
		for range 2 {
			if _, err := svc.Settle(ctx, tx, *charge, unpaid); err != nil {
				return err
			}
		}
		for range 2 {
			if _, err := svc.Cancel(ctx, tx, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the commands: %v", err)
	}

	for _, name := range []string{contracts.EventSubscribed, contracts.EventPastDue, contracts.EventCancelled} {
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

// TestOneSubscriptionPerTenant is the invariant the routes are shaped around,
// and it is the database that keeps it: the unique index in migrations/000016
// refuses a second row whatever Go believes.
func TestOneSubscriptionPerTenant(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		plan := &contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Active: true}
		if err := crud.Create(ctx, tx, plan); err != nil {
			return err
		}
		if _, err := svc.Subscribe(ctx, tx, plan.ID); err != nil {
			return err
		}
		now := db.Now()
		second := &contracts.Subscription{
			PlanID: plan.ID, Status: contracts.StatusActive,
			CurrentPeriodStart: now, CurrentPeriodEnd: contracts.Advance(now, contracts.IntervalMonth),
		}
		return crud.Create(ctx, tx, second)
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second subscription for one tenant = %v, want a conflict", err)
	}
}

// TestARolledBackCommandLeavesNothing: a transaction that does not commit
// leaves neither the subscription nor its event, so a subscriber can never be
// told about a change that did not happen.
func TestARolledBackCommandLeavesNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	var plan uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		p := &contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Active: true}
		if err := crud.Create(ctx, tx, p); err != nil {
			return err
		}
		plan = p.ID
		return nil
	})
	if err != nil {
		t.Fatalf("seed the plan: %v", err)
	}

	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Subscribe(ctx, tx, plan); err != nil {
			return err
		}
		return context.Canceled // whatever went wrong after the command
	})

	var subs, events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM billing_subscriptions`).Scan(&subs); err != nil {
		t.Fatalf("count the subscriptions: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if subs != 0 || events != 0 {
		t.Errorf("after the rollback there are %d subscriptions and %d events; want none of either", subs, events)
	}
}

// TestTheNightlyRenewalChargesOutsideEveryTransaction drives the job the way
// the worker does, across two tenants, and checks the property the whole shape
// exists for: the provider is called with no transaction open.
//
// It also checks the second pass, which is the ordinary case — the job runs
// every night, and a customer whose card is dead must not be an event a night.
func TestTheNightlyRenewalChargesOutsideEveryTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	svc := internal.NewService()

	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			plan := &contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Active: true}
			if err := crud.Create(ctx, tx, plan); err != nil {
				return err
			}
			if _, err := svc.Subscribe(ctx, tx, plan.ID); err != nil {
				return err
			}
			return tx.DB().Exec(
				`UPDATE billing_subscriptions SET current_period_start = now() - interval '32 days',
					current_period_end = now() - interval '1 day'`).Error
		})
		if err != nil {
			t.Fatalf("seed %s: %v", tenant.Slug, err)
		}
	}

	provider := &spy{admin: admin}
	job := internal.Renew(lister{acme, globex}, svc, provider, time.Minute)
	for pass := range 2 {
		if err := job.Run(t.Context(), conn); err != nil {
			t.Fatalf("renewal pass %d: %v", pass, err)
		}
	}
	if job.Name != "billing-renew" {
		t.Errorf("the job is called %q; the name is its advisory lock and has to be stable", job.Name)
	}

	// Two tenants, two nights: four charges, and every one of them made with no
	// transaction of this connection open. The second night charges again —
	// asking a dead card twice is right — and publishes nothing, because the
	// subscription was already past due.
	if provider.calls != 4 {
		t.Errorf("the provider was asked %d times, want one per tenant per night", provider.calls)
	}
	if provider.inTransaction > 0 {
		t.Errorf("%d charges were taken with a transaction open; a payment processor must never hold one", provider.inTransaction)
	}
	var overdue, events int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM billing_subscriptions WHERE status = $1`, contracts.StatusPastDue).Scan(&overdue); err != nil {
		t.Fatalf("count the past-due subscriptions: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1`, contracts.EventPastDue).Scan(&events); err != nil {
		t.Fatalf("count the events: %v", err)
	}
	if overdue != 2 || events != 2 {
		t.Errorf("%d past-due subscriptions and %d events; want one per tenant, once", overdue, events)
	}
}

// spy is a PaymentProvider that settles nothing and counts how many charges
// were taken while some transaction of this application was open.
type spy struct {
	admin         *sql.DB
	calls         int
	inTransaction int
}

func (s *spy) Charge(ctx context.Context, c contracts.Charge) (contracts.Receipt, error) {
	s.calls++
	// idle in transaction is what a backend looks like between a BEGIN and its
	// COMMIT. If the renewal held one across this call there would be one.
	//
	// Scoped to this test's own connections, which is what dbtest's
	// application_name is for: every test in the suite shares one database, so
	// counting every backend in it counted whatever modules/auth or modules/user
	// happened to be doing at the time — and this failed on a tree where the
	// renewal held nothing, roughly one run in two.
	var open int
	err := s.admin.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_stat_activity
		 WHERE state = 'idle in transaction' AND application_name = current_setting('search_path')`).Scan(&open)
	if err == nil {
		s.inTransaction += open
	}
	return contracts.Receipt{Reference: "spy:" + c.Subscription.String(), At: db.Now()}, nil
}

// lister is what the tenant module implements.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }
