package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The record of one person's set-password mail is read newest-first: a redacted
// reading of an invitation answers "did the last link go?" from the last row this
// kind left, which is the order contracts.MailLedger's own read takes (seq DESC).
// So a refusal between two accepted handoffs is only legible while the rows keep
// the order the handoffs happened in. This replays taken, refused, taken and asks
// for that order by name: the trail ends on the send that opened the door, with
// the one refusal between the two sends it sits between.
//
// A build that re-appended the refusal when the recovery landed, or that rewrote
// the failed row as a sent one when a later send of the same kind was taken, keeps
// the counts beside this reading satisfied — three rows, two sent, one refused —
// and answers here that the newest mail to that person did not go.
func TestTheMailTrailOfARecoveredInvitationReadsSentRefusedSentInThatOrder(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	authtest.InvitationOffer(t, f.InvitationFixture, u)
	resent := invitationRow(t, f, u.ID)
	f.Advance(contracts.ResetInterval)
	resendInvitation(t, f, u.ID, resent.Invitation.Revision)

	f.Mail.Failure = errors.New("smtp 451 4.3.0 the relay would not take it")
	authtest.InvitationOffer(t, f.InvitationFixture, invitationRow(t, f, u.ID))

	f.Mail.Failure = nil
	recovery := invitationRow(t, f, u.ID)
	f.Advance(contracts.ResetInterval)
	resendInvitation(t, f, u.ID, recovery.Invitation.Revision)
	if mailed := authtest.InvitationOffer(t, f.InvitationFixture, invitationRow(t, f, u.ID)); mailed == "" {
		t.Fatal("the resend after a refused handoff mailed no link, so the order below reads an unfinished trail")
	}

	var trail string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT coalesce(string_agg(outcome, ',' ORDER BY seq), '')`+
			` FROM direct_mail_deliveries WHERE kind = $1`, contracts.MailSetPassword).
			Row().Scan(&trail)
	}); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{notification.MailSent, notification.MailFailed, notification.MailSent}, ",")
	if trail != want {
		t.Errorf("the set-password trail reads %q in the order it was written, want the %s of the recovery after the %s the transport refused, after the first %s",
			trail, notification.MailSent, notification.MailFailed, notification.MailSent)
	}
}
