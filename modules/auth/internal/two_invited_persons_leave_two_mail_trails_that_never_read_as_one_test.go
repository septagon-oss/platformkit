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
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A redacted delivery reading answers about one invited person, so the rows behind
// that answer have to be that person's own. Two people are invited into this
// tenant, and each is handed a first link, resent and handed a second: the first
// person's second link is taken by the transport and the second person's is
// refused. Read by the address its mail was named for, each trail says what happened
// to that person's link alone — two sends for one, a send then a refusal for the
// other — and no row names the administrator who asked, whose address is in no mail
// this module sends. Every case beside this one reads these rows by kind alone, so a
// build that recorded a handoff under the acting administrator's address, or that let
// one person's rows answer the reading about another, keeps every count of this
// module's mail correct and is refused only here.
func TestTwoInvitedPersonsLeaveTwoMailTrailsThatNeverReadAsOne(t *testing.T) {
	f := invitationSQL(t)
	ada := authtest.InvitationPerson(t, f.InvitationFixture)
	bo := invitedPersonAt(t, f, "another@example.com")
	if ada.Invitation.Recipient == bo.Invitation.Recipient {
		t.Fatalf("both invitations were created for one address %q, so nothing here separates two people",
			ada.Invitation.Recipient)
	}
	authtest.InvitationOffer(t, f.InvitationFixture, ada)
	authtest.InvitationOffer(t, f.InvitationFixture, bo)

	f.Advance(contracts.ResetInterval)
	resendInvitation(t, f, ada.ID, invitationRow(t, f, ada.ID).Invitation.Revision)
	authtest.InvitationOffer(t, f.InvitationFixture, invitationRow(t, f, ada.ID))

	f.Advance(contracts.ResetInterval)
	resendInvitation(t, f, bo.ID, invitationRow(t, f, bo.ID).Invitation.Revision)
	f.Mail.Failure = errors.New("smtp 451 4.3.0 the relay would not take it")
	authtest.InvitationOffer(t, f.InvitationFixture, invitationRow(t, f, bo.ID))
	f.Mail.Failure = nil

	// Each claim states the address it is about, so a trail that came back empty —
	// because the handoff above never happened — refuses rather than passing.
	if got := mailTrailOf(t, f, ada, notification.MailSent, notification.MailSent); got != "" {
		t.Errorf("the set-password trail of %s reads %s, want the two sends that named that address and no other",
			ada.Invitation.Recipient, got)
	}
	if got := mailTrailOf(t, f, bo, notification.MailSent, notification.MailFailed); got != "" {
		t.Errorf("the set-password trail of %s reads %s, want the send the transport took then the refusal it made",
			bo.Invitation.Recipient, got)
	}
	if rows := mailRowsNamed(t, f, "actor@example.com"); rows != 0 {
		t.Errorf("%d set-password mail rows name the acting administrator's address, want a trail that names only the person each link was mailed to",
			rows)
	}
}

// invitedPersonAt invites a person at a chosen address with the roles the fixture's
// own helper asks for, because a case about two trails cannot share one address.
func invitedPersonAt(t *testing.T, f invitationSQLFixture, email string) *user.User {
	t.Helper()
	var u *user.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		u, err = f.Commands.CreateInvitation(ctx, tx, user.InvitationIntent{Email: email, Roles: []string{"editor"}})
		return err
	}); err != nil {
		t.Fatalf("the invitation of %s returned %v, want a pending person a link can be mailed to", email, err)
	}
	return u
}

// mailTrailOf reads one invitation's set-password rows, by its recipient and in the
// order they were written, and answers with the trail when it is not what was wanted
// (and with "" when it is), so the refusal names the person whose reading went wrong.
func mailTrailOf(t *testing.T, f invitationSQLFixture, u *user.User, want ...string) string {
	t.Helper()
	var trail string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT coalesce(string_agg(outcome, ',' ORDER BY seq), '')`+
			` FROM direct_mail_deliveries WHERE kind = $1 AND recipient = $2`,
			contracts.MailSetPassword, u.Invitation.Recipient).Row().Scan(&trail)
	}); err != nil {
		t.Fatal(err)
	}
	if trail == strings.Join(want, ",") {
		return ""
	}
	if trail == "" {
		return "nothing — no set-password row names that address"
	}
	return trail
}

// mailRowsNamed counts the set-password rows written for one address, whatever
// outcome they carry.
func mailRowsNamed(t *testing.T, f invitationSQLFixture, address string) int64 {
	t.Helper()
	var rows int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM direct_mail_deliveries WHERE kind = $1 AND recipient = $2`,
			contracts.MailSetPassword, address).Row().Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}
