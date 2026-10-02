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

func TestAnotherActorCannotWithdrawOrReadAWithdrawnProposal(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id = uuid.Nil
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		row := propose(t, subject, svc, ctx, tx)
		id = row.ID
		_, err := svc.Withdraw(ctx, tx, id, row.Revision)
		return err
	})
	if err != nil {
		t.Fatalf("the author withdraws their own proposal: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, withdrawErr := svc.Withdraw(tenancy.WithActor(ctx, reviewer), tx, id, 2)
		if row != nil || !errors.Is(withdrawErr, crud.ErrConflict) {
			t.Errorf("another actor's withdraw returned row=%+v, error=%v; want no row and a conflict", row, withdrawErr)
		}
		if got := published(t, tx); len(got) != 2 {
			t.Errorf("the refused withdraw left %d outbox events; want the proposal and its author's withdraw", len(got))
		}
		stored, getErr := svc.Get(ctx, tx, id)
		if getErr != nil {
			return getErr
		}
		if stored.State != contracts.StateWithdrawn || stored.Revision != 2 {
			t.Errorf("another actor changed the withdrawn proposal: %+v", stored)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("checking the other actor: %v", err)
	}
}
