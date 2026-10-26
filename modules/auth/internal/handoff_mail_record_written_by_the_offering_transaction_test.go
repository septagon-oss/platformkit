package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// The record of a handoff is the transaction that mailed it's own: read in that
// same transaction, the row is there before anything commits, and the reason a
// handoff that was taken back leaves none is the rollback and not a write that
// never happened. Only the after-state cannot tell those apart, which is what
// "a rolled-back handoff leaves no mail record" would otherwise be answered by.
func TestTheHandoffsMailRecordBelongsToTheTransactionThatMailed(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	generation, delivery := u.Invitation.Generation, *u.Invitation.DeliveryID

	var inside int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries WHERE kind = $1",
			contracts.MailSetPassword).Row().Scan(&inside); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("the handoff that was rolled back returned %v", err)
	}
	if inside != 1 {
		t.Fatalf("inside the transaction that mailed, the tenant held %d set-password mail records, want the one the send wrote", inside)
	}

	var after int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries WHERE kind = $1",
			contracts.MailSetPassword).Row().Scan(&after)
	}); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Errorf("the transaction took itself back and left %d set-password mail records, want none", after)
	}
}
