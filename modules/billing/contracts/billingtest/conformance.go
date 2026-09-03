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
	"strings"
	"testing"
	"time"

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

	// Reprice changes a stored plan's price. It is here for the same reason
	// Plan is: writing a plan is kit/rest's five routes and not part of the
	// lifecycle, and the lifecycle's promise is about what happens next.
	Reprice func(plan uuid.UUID, cents int64, currency string)

	// Age moves the dunning clock back, so a grace period that lasts a week can
	// be run out in a test. It is the same kind of thing Expire is: time is the
	// one input the interface has no command for.
	Age func(days int)

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
		// The review's escape probe, as a case. A tenant that owed money moved
		// itself to a plan it had created at a price of nought, and the debt
		// went with the old price. The half of the fix that lives in this
		// interface is here; the other half — that a customer cannot create a
		// plan at all — is a permission, and modules/billing's own test proves
		// it over HTTP.
		"a past-due subscription cannot be moved to another plan": func(t *testing.T, f Fixture) {
			sub := subscribed(t, f, pro())
			charge := due(t, f)
			if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "r"}); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			cheap := f.Plan(&contracts.Plan{Code: "cheap", Name: "Cheap", PriceCents: 0,
				Currency: "EUR", Interval: contracts.IntervalMonth, Active: true})
			f.silent(t, "escaping a debt", func() {
				if _, err := f.Service.Subscribe(f.Ctx, f.Tx, cheap); !errors.Is(err, crud.ErrConflict) {
					t.Errorf("changing plan while past due = %v, want ErrConflict", err)
				}
			})
			back, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if back.PlanID != sub.PlanID || back.Status != contracts.StatusPastDue {
				t.Errorf("the debt did not persist: plan %v status %q", back.PlanID, back.Status)
			}
			// Subscribing to the plan it is already on is not a change and is
			// still allowed: a customer re-submitting a form is not an escape.
			if _, err := f.Service.Subscribe(f.Ctx, f.Tx, sub.PlanID); err != nil {
				t.Errorf("the same plan again while past due = %v, want it to say nothing", err)
			}
		},

		// The dunning ceiling's other escape route, and the one the review
		// exploited live: end it now, resubscribe, get a fresh period. Ending
		// it now is what a customer who owes for a period already served may
		// not do; leaving at the end of the period is what they may do.
		"a subscription that owes for a period cannot be ended in the middle of one": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "no"}); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			var err error
			f.silent(t, "walking away from a debt", func() {
				_, err = f.Service.Cancel(f.Ctx, f.Tx, false)
			})
			if !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("cancelling now while a period is outstanding = %v, want ErrConflict", err)
			}
			// The refusal names the period that is owed for, because a customer
			// told only "conflict" cannot tell which of their two problems it is.
			if want := charge.PeriodEnd.UTC().Format("2006-01-02"); !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is %q; it does not name the outstanding period %s", err, want)
			}
			still, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if still.Status != contracts.StatusPastDue {
				t.Errorf("the refused cancel left the subscription %q", still.Status)
			}
			// Leaving at the end of the period is still allowed. It is the
			// customer's decision to make and it does not cancel the debt.
			f.one(t, "leaving at the end of the period", contracts.EventCancelled, func() {
				if _, err := f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
					t.Errorf("cancelling at period end while past due = %v, want it allowed", err)
				}
			})
		},

		// The other half of the same exploit. Whatever route a subscription
		// took to cancelled, coming back must not clear the attempt count, the
		// grace clock or the period that was never paid for.
		"resubscribing carries the debt": func(t *testing.T, f Fixture) {
			plan := f.Plan(pro())
			if _, err := f.Service.Subscribe(f.Ctx, f.Tx, plan); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			charge := due(t, f)
			if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "no"}); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			// The way out that is allowed: leave at the end of the period, and
			// let the renewal that finds it expired end it.
			if _, err := f.Service.Cancel(f.Ctx, f.Tx, true); err != nil {
				t.Fatalf("Cancel at period end: %v", err)
			}
			if _, _, err := f.Service.Renew(f.Ctx, f.Tx); err != nil {
				t.Fatalf("Renew: %v", err)
			}
			gone, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if gone.Status != contracts.StatusCancelled {
				t.Fatalf("the subscription is %q, want it ended", gone.Status)
			}
			var back *contracts.Subscription
			f.one(t, "coming back", contracts.EventSubscribed, func() {
				if back, err = f.Service.Subscribe(f.Ctx, f.Tx, plan); err != nil {
					t.Fatalf("Subscribe again: %v", err)
				}
			})
			switch {
			case back.Status != contracts.StatusPastDue:
				t.Errorf("status is %q, want %q: a tenant that owed when it left owes when it comes back",
					back.Status, contracts.StatusPastDue)
			case back.AttemptCount != 1:
				t.Errorf("the attempt count is %d, want the 1 it had", back.AttemptCount)
			case back.PastDueSince == nil:
				t.Error("resubscribing restarted the grace clock")
			case !back.CurrentPeriodEnd.Equal(charge.PeriodEnd):
				t.Error("resubscribing gave a fresh period; the outstanding one was never paid for")
			}
			// And the very next renewal asks for the same money, under the same
			// key, rather than serving a period nobody paid for.
			_, owed, err := f.Service.Renew(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Renew: %v", err)
			}
			if owed == nil || owed.IdempotencyKey != charge.IdempotencyKey {
				t.Errorf("after coming back the renewal owes %v, want the same charge", owed)
			}
		},

		// The billing day is the subscription's, not the last period's. Without
		// a stored anchor a period clamped to the 28th of February anchored on
		// the 28th, and the 31st never came back.
		"the billing day is anchored on the subscription": func(t *testing.T, f Fixture) {
			sub := subscribed(t, f, free())
			anchor := sub.CurrentPeriodStart.Day()
			if sub.AnchorDay != anchor {
				t.Fatalf("the anchor is %d, want the %d the subscription started on", sub.AnchorDay, anchor)
			}
			f.Expire()
			renewed, _, err := f.Service.Renew(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Renew: %v", err)
			}
			if renewed.AnchorDay != anchor {
				t.Errorf("renewing moved the anchor to %d", renewed.AnchorDay)
			}
			// The period lands on the anchor, or on the last day of a month too
			// short to have it — and never on anything else.
			end := renewed.CurrentPeriodEnd
			last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location()).AddDate(0, 1, -1).Day()
			if want := min(anchor, last); end.Day() != want {
				t.Errorf("the period ends on the %d of %s, want the %d", end.Day(), end.Month(), want)
			}
		},

		// A charge carries a key derived from what is being paid for and the
		// period, so a renewal that runs again after a crash presents the same
		// key and a provider that honours it takes the money once.
		"a charge is idempotent in what it asks for": func(t *testing.T, f Fixture) {
			sub := subscribed(t, f, pro())
			first := due(t, f)
			want := contracts.Key(sub.ID, first.PeriodEnd)
			if first.IdempotencyKey != want {
				t.Errorf("the key is %q, want %q", first.IdempotencyKey, want)
			}
			if first.Subject != sub.ID {
				t.Errorf("the charge is for %v, want the subscription %v", first.Subject, sub.ID)
			}
			// Asked again for the same unpaid period: the same key, because
			// nothing about the charge has changed.
			var again *contracts.Charge
			f.silent(t, "asking a second time", func() {
				var err error
				if _, again, err = f.Service.Renew(f.Ctx, f.Tx); err != nil {
					t.Fatalf("Renew: %v", err)
				}
			})
			if again == nil || again.IdempotencyKey != first.IdempotencyKey {
				t.Errorf("the second ask carries %v, want the same key", again)
			}
		},

		// A provider that cannot yet say is neither paid nor unpaid.
		"a pending receipt leaves the subscription where it is": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			f.silent(t, "a pending charge", func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "p", Pending: true}); err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
			waiting, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if waiting.Status == contracts.StatusPastDue {
				t.Error("a customer whose payment is in flight was marked past due")
			}
			if !waiting.CurrentPeriodEnd.Equal(charge.PeriodEnd) {
				t.Error("a pending payment started the next period")
			}
			// And the same charge, settled the next night, is the renewal.
			f.one(t, "settling what was pending", contracts.EventRenewed, func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "p", Settled: true}); err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
		},

		// Dunning has a ceiling. Without one a dead card was retried every
		// night for as long as the installation ran, and the customer was
		// served for as long as it was retried.
		"a grace period that runs out cancels the subscription": func(t *testing.T, f Fixture) {
			subscribed(t, f, pro())
			charge := due(t, f)
			f.one(t, "the first refusal", contracts.EventPastDue, func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "no"}); err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
			// The nights in between say nothing and count.
			f.silent(t, "the second refusal", func() {
				if _, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "no"}); err != nil {
					t.Fatalf("Settle: %v", err)
				}
			})
			counted, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if counted.AttemptCount != 2 || counted.PastDueSince == nil {
				t.Errorf("after two refusals the dunning state is %d attempts since %v", counted.AttemptCount, counted.PastDueSince)
			}
			// Inside the grace period the renewal still asks for the money.
			if _, still, err := f.Service.Renew(f.Ctx, f.Tx); err != nil || still == nil {
				t.Errorf("inside the grace period the renewal owes %v (%v)", still, err)
			}
			f.Age(contracts.GraceDays + 1)
			f.one(t, "the grace period running out", contracts.EventCancelled, func() {
				_, owed, err := f.Service.Renew(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Renew: %v", err)
				}
				if owed != nil {
					t.Error("a subscription past its grace period is still being charged")
				}
			})
			ended, err := f.Service.Current(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if ended.Status != contracts.StatusCancelled {
				t.Errorf("after the grace period the status is %q, want %q", ended.Status, contracts.StatusCancelled)
			}
		},

		// The price is stamped on the subscription at subscribe and at every
		// renewal, and the charge is made from the stamp. Re-pricing a plan
		// used to change what every live subscriber was billed that night, and
		// changing its currency billed them in another one.
		"re-pricing a plan applies from the next period": func(t *testing.T, f Fixture) {
			sub := subscribed(t, f, pro())
			if sub.PriceCents != 2900 || sub.Currency != "EUR" {
				t.Fatalf("the subscription was stamped %d %s", sub.PriceCents, sub.Currency)
			}
			f.Reprice(sub.PlanID, 9900, "USD")
			charge := due(t, f)
			if charge.AmountCents != 2900 || charge.Currency != "EUR" {
				t.Errorf("the period already served was charged %d %s at the new price", charge.AmountCents, charge.Currency)
			}
			after, err := f.Service.Settle(f.Ctx, f.Tx, charge, contracts.Receipt{Reference: "r", Settled: true})
			if err != nil {
				t.Fatalf("Settle: %v", err)
			}
			if after.PriceCents != 9900 || after.Currency != "USD" {
				t.Errorf("the next period is stamped %d %s, want the plan's new price", after.PriceCents, after.Currency)
			}
		},

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

		"resubscribing after it ended does not reissue the trial": func(t *testing.T, f Fixture) {
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
			// Not a trial. The trial is once per tenant, ever: cancelling and
			// resubscribing four times used to be four free periods, and the
			// review counted them.
			case back.Status != contracts.StatusActive:
				t.Errorf("status is %q, want %q", back.Status, contracts.StatusActive)
			case back.TrialUsedAt == nil:
				t.Error("the trial this tenant already had is not recorded")
			// The period it is given has already ended, which is what "a charge
			// is due" means in a module whose only way to take money is the
			// nightly renewal: the next run charges for the period it is about
			// to serve rather than serving one free.
			case back.CurrentPeriodEnd.After(db.Now()):
				t.Error("resubscribing served another free period")
			case !back.CurrentPeriodEnd.After(back.CurrentPeriodStart):
				t.Error("the new period does not end after it starts")
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
