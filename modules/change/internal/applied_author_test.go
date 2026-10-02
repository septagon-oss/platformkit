package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

func TestTheProposerCannotReadTheirAppliedProposalByRetryingApply(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row := propose(t, subject, svc, tenancy.WithActor(ctx, proposer), tx)
		id = row.ID
		decider := tenancy.WithActor(ctx, reviewer)
		approved, reviewErr := svc.Review(decider, tx, id, contracts.Review{
			Verdict: contracts.VerdictApproved, ExpectedRevision: 1,
		})
		if reviewErr != nil {
			return reviewErr
		}
		_, applyErr := svc.Apply(decider, tx, id, approved.Revision)
		return applyErr
	})
	if err != nil {
		t.Fatalf("another account decides and applies the proposal: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, applyErr := svc.Apply(tenancy.WithActor(ctx, proposer), tx, id, 3)
		if row != nil || !errors.Is(applyErr, contracts.ErrSelfReview) {
			t.Errorf("the author retried another account's apply and received row=%+v, error=%v; want no row and the self-review refusal", row, applyErr)
		}
		if subject.saves != 1 {
			t.Errorf("the retry saved the subject %d times; want one", subject.saves)
		}
		if got := published(t, tx); len(got) != 3 {
			t.Errorf("the retry left %d events; want propose, review and apply once", len(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("checking the author's retry: %v", err)
	}
}
