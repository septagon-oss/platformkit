package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// An applied proposal is over: no verdict reverses it, its author cannot withdraw
// it, and a second apply answers with the row without writing the subject again.
// Each refusal returns no row, and none of them adds to the outbox.
func TestAnAppliedProposalTakesNoFurtherDecision(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	id := uuid.Nil
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row := propose(t, subject, svc, tenancy.WithActor(ctx, proposer), tx)
		id = row.ID
		row, err := svc.Review(tenancy.WithActor(ctx, reviewer), tx, id,
			contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: row.Revision})
		if err != nil {
			return err
		}
		_, err = svc.Apply(tenancy.WithActor(ctx, reviewer), tx, id, row.Revision)
		return err
	})
	if err != nil {
		t.Fatalf("propose, approve and apply: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, try := range []struct {
			who uuid.UUID
			in  contracts.Review
			why string
		}{
			{secondDecider, contracts.Review{Verdict: contracts.VerdictDeclined, ExpectedRevision: 3}, "another decider declining it"},
			{reviewer, contracts.Review{Verdict: contracts.VerdictDeclined, ExpectedRevision: 3}, "the reviewer reversing their verdict"},
		} {
			row, reviewErr := svc.Review(tenancy.WithActor(ctx, try.who), tx, id, try.in)
			if row != nil || !errors.Is(reviewErr, crud.ErrConflict) {
				t.Errorf("%s returned row=%+v, error=%v; want no row and a conflict", try.why, row, reviewErr)
			}
		}
		row, withdrawErr := svc.Withdraw(tenancy.WithActor(ctx, proposer), tx, id, 3)
		if row != nil || !errors.Is(withdrawErr, crud.ErrConflict) {
			t.Errorf("the author's withdraw returned row=%+v, error=%v; want no row and a conflict", row, withdrawErr)
		}
		row, applyErr := svc.Apply(tenancy.WithActor(ctx, secondDecider), tx, id, 3)
		if applyErr != nil || row == nil || row.State != contracts.StateApplied || row.Revision != 3 {
			t.Errorf("a second apply returned row=%+v, error=%v; want the applied row at revision 3", row, applyErr)
		}
		if subject.saves != 1 {
			t.Errorf("the subject was written %d times; want the one apply", subject.saves)
		}
		if got := published(t, tx); len(got) != 3 {
			t.Errorf("the later commands left %d outbox events; want proposed, reviewed and applied", len(got))
		}
		stored, getErr := svc.Get(ctx, tx, id)
		if getErr != nil {
			return getErr
		}
		if stored.State != contracts.StateApplied || stored.Verdict != contracts.VerdictApproved || stored.Revision != 3 {
			t.Errorf("a later command changed the applied proposal: %+v", stored)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the applied proposal: %v", err)
	}
}
