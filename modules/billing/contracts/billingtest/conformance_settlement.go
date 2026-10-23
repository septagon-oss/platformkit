package billingtest

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

func retainPendingSubscription(t *testing.T, f Fixture) {
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
}

func cancelAfterGrace(t *testing.T, f Fixture) {
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
}

func settleNextPeriod(t *testing.T, f Fixture) {
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
}

func markPastDue(t *testing.T, f Fixture) {
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
}

func ignoreOldReceipt(t *testing.T, f Fixture) {
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
}
