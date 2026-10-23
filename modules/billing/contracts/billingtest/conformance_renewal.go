package billingtest

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

func anchorBillingDay(t *testing.T, f Fixture) {
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
}

func repeatCharge(t *testing.T, f Fixture) {
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
}

func repriceNextPeriod(t *testing.T, f Fixture) {
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
}

func retainRunningPeriod(t *testing.T, f Fixture) {
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
}

func renewFreePlan(t *testing.T, f Fixture) {
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
}

func chargePaidPlan(t *testing.T, f Fixture) {
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
}
