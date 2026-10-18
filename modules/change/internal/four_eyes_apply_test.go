package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// TestTheProposerMayNotApplyTheirOwnChange is the second half of four eyes.
//
// The contract states it twice and the README once more: contracts.Service says
// Apply "refuses unless … the actor is not the proposer", contracts/permissions.go
// says the grant is not the rule because "a grant two people both hold is exactly
// the case that rule is about", and modules/change/README.md's Authorization
// section promises "Reviewer ≠ proposer and applier ≠ proposer are checked inside
// each command against the row's own proposer".
//
// The one thing that made that sentence checkable was missing: every case in this
// package applied as the reviewer, so deleting the applier check from Service.Apply
// left the suite green — the rule that stops an account with both grants from
// proposing and writing its own change was carried by a line no test could remove.
// A reviewer who holds change:decide can already approve; if the apply also let the
// proposer through, one account holding both grants would be a complete approval
// chain, which is the configuration the object exists to refuse.
//
// The refusal has to be ErrSelfReview, write nothing and publish nothing, and the
// proposal has to stay approved for the second account that was always going to be
// the one asking.
func TestTheProposerMayNotApplyTheirOwnChange(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		row := propose(t, subject, svc, ctx, tx)
		decided, err := svc.Review(tenancy.WithActor(ctx, reviewer), tx, row.ID,
			contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: row.Revision})
		if err != nil {
			t.Fatalf("review: %v", err)
		}

		_, err = svc.Apply(ctx, tx, row.ID, decided.Revision)
		if err == nil {
			return errors.New("the proposer applied their own change")
		}
		if !errors.Is(err, contracts.ErrSelfReview) {
			return errors.New("applying one's own proposal refused with the wrong reason: " + err.Error())
		}
		if subject.saves != 0 {
			return errors.New("the refused apply wrote the subject")
		}
		if got := published(t, tx); len(got) != 2 {
			return errors.New("the refused apply published " + got[1])
		}
		still, err := svc.Get(ctx, tx, row.ID)
		if err != nil {
			return err
		}
		if still.State != contracts.StateApproved {
			return errors.New("a refused apply left the proposal " + still.State)
		}

		// And the second account still can: the refusal is about who asked, not about
		// the row being spoiled by asking.
		applied, err := svc.Apply(tenancy.WithActor(ctx, reviewer), tx, row.ID, still.Revision)
		if err != nil {
			return errors.New("the reviewer could not apply what the proposer was refused: " + err.Error())
		}
		if applied.State != contracts.StateApplied || subject.saves != 1 {
			return errors.New("the apply the rule protects did not happen")
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}
