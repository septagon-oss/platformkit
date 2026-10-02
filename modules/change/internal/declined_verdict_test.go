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

func TestADeclinedProposalCannotBeApproved(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		proposed := propose(t, subject, svc, tenancy.WithActor(ctx, proposer), tx)
		id = proposed.ID
		_, err := svc.Review(tenancy.WithActor(ctx, reviewer), tx, id, contracts.Review{
			Verdict: contracts.VerdictDeclined, ExpectedRevision: proposed.Revision,
		})
		return err
	})
	if err != nil {
		t.Fatalf("record the first verdict: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, actor := range []uuid.UUID{reviewer, uuid.MustParse("01990000-0000-7000-8000-0000000000c3")} {
			row, reviewErr := svc.Review(tenancy.WithActor(ctx, actor), tx, id, contracts.Review{
				Verdict: contracts.VerdictApproved, ExpectedRevision: 2,
			})
			if row != nil || !errors.Is(reviewErr, crud.ErrConflict) {
				t.Errorf("a second verdict from %s returned row=%+v, error=%v; want no row and a conflict", actor, row, reviewErr)
			}
		}
		stored, err := svc.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if stored.State != contracts.StateDeclined || stored.Verdict != contracts.VerdictDeclined ||
			stored.Revision != 2 || stored.Reviewer == nil || *stored.Reviewer != reviewer {
			t.Errorf("a later verdict changed the declined proposal: %+v", stored)
		}
		if got := published(t, tx); len(got) != 2 {
			t.Errorf("the later verdicts left %d outbox events; want one proposal and one review", len(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the declined proposal: %v", err)
	}
}
