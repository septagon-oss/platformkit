package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// One mailed handoff leaves one record and one live credential, and two refused
// handoffs leave two records and none. The reading between those two is the one a
// resend is: the second send is accepted by the transport and it shares the row the
// first minted, because the single-pending rule keys it by tenant and person. This
// asks what two accepted handoffs of one invitation leave behind — a record per
// handoff, no failure claimed beside either, and one live credential, the one the
// later link carries, with the obsolete one gone from the table rather than merely
// refused at the door.
func TestTwoMailedHandoffsLeaveOneLiveCredentialAndARecordForEach(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	obsolete := authtest.InvitationOffer(t, f.InvitationFixture, u)
	f.Advance(contracts.ResetInterval)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, u.ID, u.Invitation.Revision)
		return err
	}); err != nil {
		t.Fatalf("the resend returned %v, want the current revision accepted", err)
	}
	var resent *usercontracts.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		resent, err = f.Users.Get(ctx, tx, u.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	current := authtest.InvitationOffer(t, f.InvitationFixture, resent)
	sent := f.Mail.Sent()
	if len(sent) != 2 || obsolete == "" || current == "" ||
		!strings.Contains(sent[len(sent)-1].Body, "?token=") {
		t.Fatalf("%d mailed links carrying %q, want the two accepted handoffs each mailing a link with its credential",
			len(sent), authtest.TokenIn(sent[len(sent)-1].Body))
	}

	var records, events, live, keeping, retired int64
	var outcomes string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*), coalesce(string_agg(DISTINCT outcome, ''), '') FROM direct_mail_deliveries WHERE kind = $1",
			contracts.MailSetPassword).Row().Scan(&records, &outcomes); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = $1", contracts.EventMailFailed).
			Row().Scan(&events); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&live); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM password_tokens WHERE token_hash = $1", contracts.Hash(current)).
			Row().Scan(&keeping); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens WHERE token_hash = $1", contracts.Hash(obsolete)).
			Row().Scan(&retired)
	}); err != nil {
		t.Fatal(err)
	}
	if records != 2 || outcomes != notification.MailSent {
		t.Errorf("%d records of outcome %q, want two sent rows, one per handoff the transport took", records, outcomes)
	}
	if events != 0 {
		t.Errorf("%d mail-failure events beside two handoffs the transport took, want none", events)
	}
	if live != 1 || keeping != 1 || retired != 0 {
		t.Errorf("%d credential rows, %d carrying the resent link and %d carrying the obsolete one,"+
			" want one live credential, the one the later link carries", live, keeping, retired)
	}
}
