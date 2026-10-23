package billingtest

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

func cancelAtPeriodEnd(t *testing.T, f Fixture) {
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
}

func cancelImmediately(t *testing.T, f Fixture) {
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
}

func accelerateCancellation(t *testing.T, f Fixture) {
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
}

func endCancelledPeriod(t *testing.T, f Fixture) {
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
}
