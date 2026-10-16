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

// A third account that may decide, beside the proposer and the reviewer.
var secondDecider = uuid.MustParse("01990000-0000-7000-8000-0000000000c3")

// An approved proposal has been decided: the states are proposed → approved →
// applied, and the verdict is on the record. Another decider's verdict, or the
// reviewer's own different one, is a conflict that writes nothing and publishes
// nothing; only the reviewer's replay of the same verdict answers with the row.
func TestAnApprovedProposalCannotBeDecidedAgain(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id = uuid.Nil
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row := propose(t, subject, svc, tenancy.WithActor(ctx, proposer), tx)
		id = row.ID
		_, err := svc.Review(tenancy.WithActor(ctx, reviewer), tx, id,
			contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: row.Revision})
		return err
	})
	if err != nil {
		t.Fatalf("propose and approve: %v", err)
	}

	approve := contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 2}
	decline := contracts.Review{Verdict: contracts.VerdictDeclined, ExpectedRevision: 2}
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, try := range []struct {
			who uuid.UUID
			in  contracts.Review
			why string
		}{
			{secondDecider, approve, "another decider approving again"},
			{secondDecider, decline, "another decider declining the approved proposal"},
			{reviewer, decline, "the reviewer reversing their own verdict"},
		} {
			row, reviewErr := svc.Review(tenancy.WithActor(ctx, try.who), tx, id, try.in)
			if row != nil || !errors.Is(reviewErr, crud.ErrConflict) {
				t.Errorf("%s returned row=%+v, error=%v; want no row and a conflict", try.why, row, reviewErr)
			}
		}
		row, reviewErr := svc.Review(tenancy.WithActor(ctx, proposer), tx, id, approve)
		if row != nil || !errors.Is(reviewErr, contracts.ErrSelfReview) {
			t.Errorf("the author's verdict returned row=%+v, error=%v; want no row and the self-review refusal", row, reviewErr)
		}
		row, reviewErr = svc.Review(tenancy.WithActor(ctx, reviewer), tx, id, approve)
		if reviewErr != nil || row == nil || row.Revision != 2 {
			t.Errorf("the reviewer's own replay returned row=%+v, error=%v; want the decided row at revision 2", row, reviewErr)
		}
		if got := published(t, tx); len(got) != 2 {
			t.Errorf("the later verdicts left %d outbox events; want the proposal and its one verdict", len(got))
		}
		stored, getErr := svc.Get(ctx, tx, id)
		if getErr != nil {
			return getErr
		}
		if stored.State != contracts.StateApproved || stored.Verdict != contracts.VerdictApproved ||
			stored.Revision != 2 || stored.Reviewer == nil || *stored.Reviewer != reviewer {
			t.Errorf("a later verdict changed the decided proposal: %+v", stored)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("checking the later verdicts: %v", err)
	}
}
