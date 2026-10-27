package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// A handoff the transport took is the other half of the rule the refused ones
// pin: one record saying it was sent, the credential it mailed still live, and
// no failure event beside it at all. The event door is what a reader of the
// trail follows, so a build that said "the mail failed" beside a mail the
// transport took would be false in the one place nobody rechecks.
func TestAMailedHandoffLeavesOneSentRecordAndNoFailureEvent(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatalf("the handoff returned %v, want the accepted send acknowledged", err)
	}
	sent := f.Mail.Sent()
	if len(sent) != 1 || authtest.TokenIn(sent[0].Body) == "" {
		t.Fatalf("%d mailed links, want the one this handoff mailed", len(sent))
	}

	var records, events, live int64
	var outcome string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*), coalesce(max(outcome), '') FROM direct_mail_deliveries WHERE kind = $1",
			contracts.MailSetPassword).Row().Scan(&records, &outcome); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = $1",
			contracts.EventMailFailed).Row().Scan(&events); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&live)
	}); err != nil {
		t.Fatal(err)
	}
	if records != 1 || outcome != notification.MailSent {
		t.Errorf("%d records of outcome %q, want one row saying the send was taken", records, outcome)
	}
	if events != 0 {
		t.Errorf("%d mail-failure events beside a mail the transport took, want none", events)
	}
	if live != 1 {
		t.Errorf("%d credential rows beside an accepted send, want the one link that was mailed", live)
	}
}
