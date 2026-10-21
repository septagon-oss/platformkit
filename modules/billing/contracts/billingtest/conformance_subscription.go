package billingtest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

func readEmptySubscription(t *testing.T, f Fixture) {
	for what, call := range map[string]func() error{
		"Current": func() error { _, err := f.Service.Current(f.Ctx, f.Tx); return err },
		"Cancel":  func() error { _, err := f.Service.Cancel(f.Ctx, f.Tx, false); return err },
		"Renew":   func() error { _, _, err := f.Service.Renew(f.Ctx, f.Tx); return err },
	} {
		if err := call(); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("%s with no subscription = %v, want ErrNotFound", what, err)
		}
	}
}

func startTrial(t *testing.T, f Fixture) {
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
}

func repeatSubscription(t *testing.T, f Fixture) {
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
}

func changePlanWithinPeriod(t *testing.T, f Fixture) {
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
}

func refuseInactivePlan(t *testing.T, f Fixture) {
	closed := pro()
	closed.Code, closed.Active = "legacy", false
	_, err := f.Service.Subscribe(f.Ctx, f.Tx, f.Plan(closed))
	mustBe(t, err, crud.ErrConflict)
}

func refuseUnknownPlan(t *testing.T, f Fixture) {
	_, err := f.Service.Subscribe(f.Ctx, f.Tx, uuid.New())
	mustBe(t, err, crud.ErrNotFound)
}

func resubscribeWithoutTrial(t *testing.T, f Fixture) {
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
	// A trial is once per tenant; resubscribing must not issue another.
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
}
