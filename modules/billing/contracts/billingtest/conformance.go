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
		// A plan change must not erase an unpaid period. A tenant that owed money moved
		// itself to a plan it had created at a price of nought, and the debt went with the
		// old price. The half of the rule that lives in this interface is here; the other
		// half — that a customer cannot create a plan at all — is a permission, which the
		// module's own test proves over HTTP.
		"a past-due subscription cannot be moved to another plan": refuseIndebtedPlanChange,
		// Debt allows cancellation at period end, but never an immediate reset: ending it
		// now, resubscribing and getting a fresh period is the ceiling's other escape
		// route. Ending it now is what a customer who owes for a period already served may
		// not do; leaving at the end of the period is what they may do.
		"a subscription that owes for a period cannot be ended in the middle of one": refuseIndebtedImmediateCancellation,
		// The other half of the same exploit. Whatever route a subscription
		// took to cancelled, coming back must not clear the attempt count, the
		// grace clock or the period that was never paid for.
		"resubscribing carries the debt": retainDebtOnResubscribe,
		// The billing day is the subscription's, not the last period's. Without
		// a stored anchor a period clamped to the 28th of February anchored on
		// the 28th, and the 31st never came back.
		"the billing day is anchored on the subscription": anchorBillingDay,
		// A charge carries a key derived from what is being paid for and the
		// period, so a renewal that runs again after a crash presents the same
		// key and a provider that honours it takes the money once.
		"a charge is idempotent in what it asks for": repeatCharge,
		// A provider that cannot yet say is neither paid nor unpaid.
		"a pending receipt leaves the subscription where it is": retainPendingSubscription,
		// Dunning has a ceiling. Without one a dead card was retried every
		// night for as long as the installation ran, and the customer was
		// served for as long as it was retried.
		"a grace period that runs out cancels the subscription": cancelAfterGrace,
		// The price is stamped on the subscription at subscribe and at every
		// renewal, and the charge is made from the stamp. Re-pricing a plan
		// used to change what every live subscriber was billed that night, and
		// changing its currency billed them in another one.
		"re-pricing a plan applies from the next period":               repriceNextPeriod,
		"a tenant that has never subscribed has no subscription":       readEmptySubscription,
		"subscribing starts a trial period":                            startTrial,
		"subscribing to the same plan again changes nothing":           repeatSubscription,
		"changing plan keeps the period":                               changePlanWithinPeriod,
		"an inactive plan takes no new subscriptions":                  refuseInactivePlan,
		"an unknown plan is not found":                                 refuseUnknownPlan,
		"cancelling at period end keeps the period":                    cancelAtPeriodEnd,
		"cancelling now ends it, once":                                 cancelImmediately,
		"ending now after asking to end later is a second decision":    accelerateCancellation,
		"a period still running is not renewed":                        retainRunningPeriod,
		"a free plan renews itself":                                    renewFreePlan,
		"a paid plan reports what is owed and changes nothing":         chargePaidPlan,
		"a settled receipt starts the next period, chained":            settleNextPeriod,
		"an unsettled receipt is past due, and says so once":           markPastDue,
		"a receipt for a period that has passed is ignored":            ignoreOldReceipt,
		"a cancelled subscription ends when its period does, silently": endCancelledPeriod,
		"resubscribing after it ended does not reissue the trial":      resubscribeWithoutTrial,
	}
}

func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
