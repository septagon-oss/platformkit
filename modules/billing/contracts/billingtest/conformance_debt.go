package billingtest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

func refuseIndebtedPlanChange(t *testing.T, f Fixture) {
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
}

func refuseIndebtedImmediateCancellation(t *testing.T, f Fixture) {
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
}

func retainDebtOnResubscribe(t *testing.T, f Fixture) {
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
}
