// Package billingtest is the conformance suite for contracts.Service, and a
// fake that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification of the subscription lifecycle written as executable cases; the
// real service and the fake both run it, so "the fake behaves like the real
// thing" is a test result rather than a hope.
package billingtest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and the two things the suite cannot reach through the interface.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service

	// Plan stores a plan and returns the id it was given. Creating a plan is
	// kit/rest's five routes and not part of the lifecycle, so it is here.
	Plan func(*contracts.Plan) uuid.UUID

	// Expire moves the subscription's period into the past. It is the other
	// thing the interface cannot do: a period ends by the clock, and a test
	// cannot wait a month for one.
	Expire func()

	// Published is the events the implementation has published so far, in
	// order. The fake returns what it recorded; the real service's harness
	// reads the outbox rows its transaction has written.
	//
	// It is part of the fixture because half of what the lifecycle promises is
	// silence: a nightly job that publishes every night is a customer told
	// every night that their card is still dead.
	Published func() []string
}

// silent runs step and fails if it published anything.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
}

// one runs step and fails unless it published exactly want.
func (f Fixture) one(t *testing.T, what, want string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != want {
		t.Errorf("%s published %v, want [%s]", what, got, want)
	}
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real service's fixture is a transaction, and a transaction
// is a scope somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// pro is a plan somebody pays for; free is one nobody does.
func pro() *contracts.Plan {
	return &contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Interval: contracts.IntervalMonth, Active: true}
}

func free() *contracts.Plan {
	return &contracts.Plan{Code: "free", Name: "Free", PriceCents: 0, Currency: "EUR", Interval: contracts.IntervalMonth, Active: true}
}

// subscribed is a fixture with a subscription to plan on it, and its id.
func subscribed(t *testing.T, f Fixture, plan *contracts.Plan) *contracts.Subscription {
	t.Helper()
	sub, err := f.Service.Subscribe(f.Ctx, f.Tx, f.Plan(plan))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return sub
}

// due is the charge a renewal reports for an expired subscription.
func due(t *testing.T, f Fixture) contracts.Charge {
	t.Helper()
	f.Expire()
	var charge *contracts.Charge
	f.silent(t, "reporting what is owed", func() {
		var err error
		if _, charge, err = f.Service.Renew(f.Ctx, f.Tx); err != nil {
			t.Fatalf("Renew: %v", err)
		}
	})
	if charge == nil {
		t.Fatal("a paid plan whose period has run out owes something")
	}
	return *charge
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"a tenant that has never subscribed has no subscription": func(t *testing.T, f Fixture) {
			for what, call := range map[string]func() error{
				"Current": func() error { _, err := f.Service.Current(f.Ctx, f.Tx); return err },
				"Cancel":  func() error { _, err := f.Service.Cancel(f.Ctx, f.Tx, false); return err },
				"Renew":   func() error { _, _, err := f.Service.Renew(f.Ctx, f.Tx); return err },
			} {
				if err := call(); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("%s with no subscription = %v, want ErrNotFound", what, err)
				}
			}
		},

		"subscribing starts a trial period": func(t *testing.T, f Fixture) {
			var sub *contracts.Subscription
			f.one(t, "subscribing", contracts.EventSubscribed, func() { sub = subscribed(t, f, pro()) })
			if sub.Status != contracts.StatusTrial {
				t.Errorf("status is %q, want %q: the first period is served before any money is asked for",
					sub.Status, contracts.StatusTrial)
			}
			if !sub.CurrentPeriodEnd.After(sub.CurrentPeriodStart) {
				t.Errorf("the period is %s to %s, which does not last", sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
			}
			if sub.CancelAtPeriodEnd {
				t.Error("a new subscription is already cancelled")
			}
			got, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil || got.ID != sub.ID {
				t.Errorf("Current = %v, %v, want the subscription that was just made", got, err)
			}
		},

		"subscribing to the same plan again changes nothing": func(t *testing.T, f Fixture) {
			plan := f.Plan(pro())
			first, err := f.Service.Subscribe(f.Ctx, f.Tx, plan)
			if err != nil {
				t.Fatalf("the first Subscribe: %v", err)
			}
			var again *contracts.Subscription
			f.silent(t, "subscribing to the same plan again", func() {
				if again, err = f.Service.Subscribe(f.Ctx, f.Tx, plan); err != nil {
					t.Fatalf("the second Subscribe: %v", err)
				}
			})
			if !again.CurrentPeriodEnd.Equal(first.CurrentPeriodEnd) {
				t.Error("the second Subscribe moved the period; it is the same intention twice")
			}
		},

		"changing plan keeps the period": func(t *testing.T, f Fixture) {
			first := subscribed(t, f, pro())
			other := f.Plan(&contracts.Plan{Code: "team", Name: "Team", PriceCents: 9900, Currency: "EUR", Interval: contracts.IntervalYear, Active: true})
			var moved *contracts.Subscription
			f.one(t, "changing plan", contracts.EventSubscribed, func() {
				var err error
				if moved, err = f.Service.Subscribe(f.Ctx, f.Tx, other); err != nil {
					t.Fatalf("Subscribe to the other plan: %v", err)
				}
			})
			if moved.PlanID != other {
				t.Errorf("the subscription is on %s, want %s", moved.PlanID, other)
			}
			if !moved.CurrentPeriodEnd.Equal(first.CurrentPeriodEnd) {
				t.Error("changing plan moved the period; the new price applies at the next renewal, and nobody is refunded for days already served")
			}
		},

		"an inactive plan takes no new subscriptions": func(t *testing.T, f Fixture) {
			closed := pro()
			closed.Code, closed.Active = "legacy", false
			_, err := f.Service.Subscribe(f.Ctx, f.Tx, f.Plan(closed))
			mustBe(t, err, crud.ErrConflict)
		},

		"an unknown plan is not found": func(t *testing.T, f Fixture) {
			_, err := f.Service.Subscribe(f.Ctx, f.Tx, uuid.New())
			mustBe(t, err, crud.ErrNotFound)
		},

		"cancelling at period end keeps the period": func(t *testing.T, f Fixture) {
			sub := subscribed(t, f, pro())
			var pending *contracts.Subscription
			f.one(t, "cancelling at period end", contracts.EventCancelled, func() {
				var err error
				if pending, err = f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
					t.Fatalf("Cancel: %v", err)
				}
			})
			if !pending.CancelAtPeriodEnd || pending.Status != sub.Status {
				t.Errorf("the subscription is %q/%v; the customer has left and is still owed the period they paid for",
					pending.Status, pending.CancelAtPeriodEnd)
			}
			if !pending.CurrentPeriodEnd.Equal(sub.CurrentPeriodEnd) {
				t.Error("cancelling shortened the period; cancelling is not a refund")
			}
			f.silent(t, "cancelling at period end again", func() {
				if _, err := f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
					t.Fatalf("the second Cancel: %v", err)
				}
			})
		},

		"cancelling now ends it, once": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			var ended *contracts.Subscription
			f.one(t, "cancelling now", contracts.EventCancelled, func() {
				var err error
				if ended, err = f.Service.Cancel(f.Ctx, f.Tx, false); err != nil {
					t.Fatalf("Cancel: %v", err)
				}
			})
			if ended.Status != contracts.StatusCancelled {
				t.Errorf("status is %q, want %q", ended.Status, contracts.StatusCancelled)
			}
			f.silent(t, "cancelling now again", func() {
				if _, err := f.Service.Cancel(f.Ctx, f.Tx, false); err != nil {
					t.Fatalf("the second Cancel: %v", err)
				}
			})
			// Asking to end later after it has already ended is not a retry of
			// anything: there is no period left to serve.
			_, err := f.Service.Cancel(f.Ctx, f.Tx, true)
			mustBe(t, err, crud.ErrConflict)
		},

		"ending now after asking to end later is a second decision": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			if _, err := f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
				t.Fatalf("Cancel at period end: %v", err)
			}
			var ended *contracts.Subscription
			f.one(t, "cancelling now afterwards", contracts.EventCancelled, func() {
				var err error
				if ended, err = f.Service.Cancel(f.Ctx, f.Tx, false); err != nil {
					t.Fatalf("Cancel now: %v", err)
				}
			})
			if ended.Status != contracts.StatusCancelled || ended.CancelAtPeriodEnd {
				t.Errorf("the subscription is %q/%v, want it ended outright", ended.Status, ended.CancelAtPeriodEnd)
			}
		},

		"a period still running is not renewed": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			f.silent(t, "renewing a period that is still running", func() {
				sub, charge, err := f.Service.Renew(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Renew: %v", err)
				}
				if charge != nil {
					t.Errorf("a period that has not ended owes %+v", *charge)
				}
				if sub.Status != contracts.StatusTrial {
					t.Errorf("status is %q, want it left alone", sub.Status)
				}
			})
		},

		"a free plan renews itself": func(t *testing.T, f Fixture) {
			subscribed(t, f, free())
			f.Expire()
			sub, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			var renewed *contracts.Subscription
			f.one(t, "renewing a free plan", contracts.EventRenewed, func() {
				var charge *contracts.Charge
				var err error
				if renewed, charge, err = f.Service.Renew(f.Ctx, f.Tx); err != nil {
					t.Fatalf("Renew: %v", err)
				}
				if charge != nil {
					t.Errorf("a free plan owes %+v", *charge)
				}
			})
			switch {
			case renewed.Status != contracts.StatusActive:
				t.Errorf("status is %q, want %q", renewed.Status, contracts.StatusActive)
			case !renewed.CurrentPeriodStart.Equal(sub.CurrentPeriodEnd):
				t.Error("the new period does not start where the last one ended")
			case !renewed.CurrentPeriodEnd.After(sub.CurrentPeriodEnd):
				t.Error("the period did not move")
			}
		},

		"a paid plan reports what is owed and changes nothing": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			switch {
			case charge.AmountCents != 2900 || charge.Currency != "EUR":
				t.Errorf("the charge is %d %s, want the plan's price", charge.AmountCents, charge.Currency)
			case charge.PlanCode != "pro":
				t.Errorf("the charge names plan %q", charge.PlanCode)
			}
			sub, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if sub.Status != contracts.StatusTrial || !sub.CurrentPeriodEnd.Equal(charge.PeriodEnd) {
				t.Error("reporting a charge changed the subscription; the money has not moved yet")
			}
		},

		"a settled receipt starts the next period, chained": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			var renewed *contracts.Subscription
			f.one(t, "settling a charge", contracts.EventRenewed, func() {
				var err error
				renewed, err = f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "r-1", Settled: true, At: db.Now()})
				if err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
			if renewed.Status != contracts.StatusActive {
				t.Errorf("status is %q, want %q", renewed.Status, contracts.StatusActive)
			}
			if !renewed.CurrentPeriodStart.Equal(charge.PeriodEnd) {
				t.Errorf("the new period starts at %s and the last one ended at %s; periods chain rather than drift with the hour a job ran",
					renewed.CurrentPeriodStart, charge.PeriodEnd)
			}
		},

		"an unsettled receipt is past due, and says so once": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			unpaid := contracts.Receipt{Reference: "r-1", At: db.Now()}
			var overdue *contracts.Subscription
			f.one(t, "a charge that did not settle", contracts.EventPastDue, func() {
				var err error
				if overdue, err = f.Service.Settle(f.Ctx, f.Tx, charge, unpaid); err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
			switch {
			case overdue.Status != contracts.StatusPastDue:
				t.Errorf("status is %q, want %q", overdue.Status, contracts.StatusPastDue)
			case !overdue.CurrentPeriodEnd.Equal(charge.PeriodEnd):
				t.Error("an unpaid period moved; nothing was bought")
			}
			// The job runs every night. A customer whose card is dead must not
			// be an event a night forever.
			f.silent(t, "the next night's charge, still unsettled", func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, unpaid); err != nil {
					t.Fatalf("the second Settle: %v", err)
				}
			})
		},

		"a receipt for a period that has passed is ignored": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			paid := contracts.Receipt{Reference: "r-1", Settled: true, At: db.Now()}
			if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, paid); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			// Between two transactions another replica may have renewed the
			// same subscription. The second answer is about a period that is
			// over, so it changes nothing and says nothing.
			f.silent(t, "settling the same charge twice", func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, paid); err != nil {
					t.Fatalf("the second Settle: %v", err)
				}
			})
		},

		"a cancelled subscription ends when its period does, silently": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			if _, err := f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			f.Expire()
			var ended *contracts.Subscription
			// Cancel already published billing.cancelled with this moment as
			// its EndsAt. Saying it again is one departure told twice.
			f.silent(t, "the renewal that ends a cancelled subscription", func() {
				var charge *contracts.Charge
				var err error
				if ended, charge, err = f.Service.Renew(f.Ctx, f.Tx); err != nil {
					t.Fatalf("Renew: %v", err)
				}
				if charge != nil {
					t.Errorf("a subscription that has ended owes %+v", *charge)
				}
			})
			if ended.Status != contracts.StatusCancelled {
				t.Errorf("status is %q, want %q", ended.Status, contracts.StatusCancelled)
			}
			f.silent(t, "the night after that", func() {
				if _, _, err := f.Service.Renew(f.Ctx, f.Tx); err != nil {
					t.Fatalf("the second Renew: %v", err)
				}
			})
		},

		"resubscribing after it ended starts a fresh period": func(t *testing.T, f Fixture) {
			plan := f.Plan(pro())
			old, err := f.Service.Subscribe(f.Ctx, f.Tx, plan)
			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			// The old period is over before the customer leaves, so "a fresh
			// period" is a claim about time rather than about microseconds.
			f.Expire()
			if _, err := f.Service.Cancel(f.Ctx, f.Tx, false); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			var back *contracts.Subscription
			f.one(t, "resubscribing", contracts.EventSubscribed, func() {
				if back, err = f.Service.Subscribe(f.Ctx, f.Tx, plan); err != nil {
					t.Fatalf("Subscribe again: %v", err)
				}
			})
			switch {
			case back.ID != old.ID:
				t.Error("resubscribing made a second subscription; a tenant has one")
			case back.Status != contracts.StatusTrial:
				t.Errorf("status is %q, want %q", back.Status, contracts.StatusTrial)
			case !back.CurrentPeriodEnd.After(db.Now()):
				t.Error("the old period was kept; it had already ended")
			}
		},
	}
}

func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
