package internal_test

import (
	"context"
	"errors"
	"reflect"
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

func TestStaleProposalCommandsReturnANamedRefusalWithoutEffects(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	ctx := tenancy.WithTenant(t.Context(), acme)
	var id uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row := propose(t, subject, svc, tenancy.WithActor(ctx, proposer), tx)
		id = row.ID
		_, err := svc.Review(tenancy.WithActor(ctx, reviewer), tx, id,
			contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"review", "apply", "withdraw"} {
		t.Run(command, func(t *testing.T) {
			// Commit the caller's transaction even after refusal: an error must not
			// conceal a partial write that only an HTTP rollback would undo.
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before, err := svc.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				eventsBefore := published(t, tx)
				var row *contracts.Proposal
				switch command {
				case "review":
					row, err = svc.Review(tenancy.WithActor(ctx, reviewer), tx, id,
						contracts.Review{Verdict: contracts.VerdictDeclined, ExpectedRevision: 1})
				case "apply":
					row, err = svc.Apply(tenancy.WithActor(ctx, reviewer), tx, id, 1)
				case "withdraw":
					row, err = svc.Withdraw(tenancy.WithActor(ctx, proposer), tx, id, 1)
				}
				if row != nil || !errors.Is(err, crud.ErrConflict) || !errors.Is(err, contracts.ErrStaleProposal) {
					t.Errorf("stale %s returned row=%+v, error=%v; want nil and the typed stale conflict", command, row, err)
				}
				after, err := svc.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(eventsBefore, published(t, tx)) || subject.saves != 0 {
					t.Errorf("stale %s changed the proposal, its outbox, or its subject", command)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
