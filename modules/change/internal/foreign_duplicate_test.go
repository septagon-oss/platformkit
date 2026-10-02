package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// A submitter who has not been granted the proposal read operation may still
// propose a known diff. That does not grant them the prior author's proposal,
// including its summary, actor and later verdict.
func TestAnotherProposerCannotReadAnOpenProposalByRepeatingItsDiff(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	svc := internal.NewService(bindingTo(newCounter()))
	input := contracts.NewProposal{
		SubjectModule: "counter", SubjectEntity: "one",
		Diff: diff(t, `{"limit":11}`), Summary: "private reason for the change",
	}
	var first *contracts.Proposal
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var createErr error
		first, createErr = svc.Propose(tenancy.WithActor(ctx, proposer), tx, input)
		return createErr
	})
	if err != nil {
		t.Fatalf("first proposal: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		other := input
		other.Summary = "a different person's reason"
		row, proposeErr := svc.Propose(tenancy.WithActor(ctx, reviewer), tx, other)
		if row != nil || !errors.Is(proposeErr, crud.ErrConflict) {
			t.Errorf("another actor's matching diff returned row=%+v, error=%v; want no row and a conflict", row, proposeErr)
		}
		if got := published(t, tx); len(got) != 1 {
			t.Errorf("the refused proposal left %d outbox events; want the original one", len(got))
		}
		stored, getErr := svc.Get(ctx, tx, first.ID)
		if getErr != nil {
			return getErr
		}
		if stored.Proposer != proposer || stored.Summary != input.Summary || stored.Revision != 1 {
			t.Errorf("another actor's submit changed the original proposal: %+v", stored)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("checking the second actor: %v", err)
	}
}
