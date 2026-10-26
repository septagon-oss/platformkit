package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The record of a handoff commits with the handoff: a transaction the worker took
// back after the transport carried the message leaves no row in the mail ledger
// beside the message it mailed, and the handoff that commits leaves exactly one,
// carrying the transport's own outcome. Without this the ledger could say a
// generation was handed over when no credential behind that sentence exists.
func TestARolledBackHandoffLeavesNoMailRecordAndTheCommittedOneLeavesOne(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	generation, delivery := u.Invitation.Generation, *u.Invitation.DeliveryID

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("the handoff that was rolled back returned %v", err)
	}
	if len(f.Mail.Sent()) != 1 {
		t.Fatalf("the rolled-back handoff left %d messages with the transport, want the one it carried", len(f.Mail.Sent()))
	}
	if outcome := mailRecordOf(t, f); outcome != "" {
		t.Errorf("the handoff that did not commit left a mail record of %q, want none", outcome)
	}

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery)
	}); err != nil {
		t.Fatalf("the next handoff of the same generation: %v", err)
	}
	if got := mailRecordOf(t, f); got != notification.MailSent+":" {
		t.Errorf("the committed handoff left a mail record of %q, want %q with no reason", got, notification.MailSent)
	}
}

// mailRecordOf answers this tenant's set-password mail records as one string, so a
// case can say both "no row" and "which outcome" without a second query.
func mailRecordOf(t *testing.T, f invitationSQLFixture) string {
	t.Helper()
	var out string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT coalesce(string_agg(outcome || ':' || coalesce(reason, ''), ','), '')"+
			" FROM direct_mail_deliveries WHERE kind = $1", contracts.MailSetPassword).Row().Scan(&out)
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
