package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A handoff the email transport refuses deletes the credential that handoff had
// just minted, so the person behind a refused link holds nothing usable. That
// deletion is a DELETE, and a DELETE says what it reaches: this asks whether it
// reaches one invited person or every invited person in the tenant. A second
// person is invited here and mailed a link of their own a moment before the
// refusal, and that link must still accept, its credential still in the table,
// while the refused person's own credential is gone and that person stays pending
// with no password. Nothing is spent and no clock moved by the time these counts
// are taken, so a row in the table is a usable link. Every case beside this one
// invites a single person, so a cleanup keyed on the invitation generation or the
// tenant rather than the person keeps each count and each refusal those cases make
// and is refused only where two invited people share one tenant.
func TestARefusedHandoffsCleanupLeavesAnotherInvitedPersonsLinkLive(t *testing.T) {
	f := invitationSQL(t)
	ada := authtest.InvitationPerson(t, f.InvitationFixture)
	adaToken := authtest.InvitationOffer(t, f.InvitationFixture, ada)
	if adaToken == "" {
		t.Fatal("the first invited person was mailed no link, so no live credential here could outlive a refusal")
	}
	bray := invitePersonNamed(t, f, "bray@example.com")
	f.Mail.Failure = errors.New("smtp 550 5.7.1 the relay refused that message")
	brayToken := authtest.InvitationOffer(t, f.InvitationFixture, bray)
	f.Mail.Failure = nil
	if brayToken == "" {
		t.Fatal("the second invited person's refused handoff mailed nothing at all, so no cleanup of a minted credential ran")
	}

	if kept := credentialsOf(t, f, ada.ID); kept != 1 {
		t.Errorf("%d credentials of %s stand after a handoff beside theirs was refused, want the link mailed to them still there",
			kept, ada.Invitation.Recipient)
	}
	if gone := credentialsOf(t, f, bray.ID); gone != 0 {
		t.Errorf("%d credentials of %s stand after their own handoff was refused, want the credential that handoff minted deleted",
			gone, bray.Invitation.Recipient)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Auth.Reset(ctx, tx, adaToken, "a sufficiently strong password", contracts.Client{})
		return err
	}); err != nil {
		t.Fatalf("%s spent the link mailed before the refusal beside it and got %v, want that acceptance to go through",
			ada.Invitation.Recipient, err)
	}
	if got := personSnapshot(t, f, ada.ID); got.Status != user.StatusActive || got.Invitation.Phase != user.InvitationAccepted {
		t.Errorf("%s reads status %q in phase %q after spending their link, want an accepted membership",
			ada.Invitation.Recipient, got.Status, got.Invitation.Phase)
	}
	if got := personSnapshot(t, f, bray.ID); got.Invitation.Phase != user.InvitationPending || got.PasswordHash != "" {
		t.Errorf("%s reads phase %q with a password set = %t after their own handoff was refused, want a pending person with no password",
			bray.Invitation.Recipient, got.Invitation.Phase, got.PasswordHash != "")
	}
}

// invitePersonNamed invites one person at a chosen address, because a case about
// the reach of one person's cleanup cannot put two people behind one address.
func invitePersonNamed(t *testing.T, f invitationSQLFixture, email string) *user.User {
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

// credentialsOf counts the set-password rows one person holds: each is a link that
// has not been spent, because spending deletes the row.
func credentialsOf(t *testing.T, f invitationSQLFixture, id uuid.UUID) int64 {
	t.Helper()
	var rows int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM password_tokens WHERE user_id = $1`, id).Row().Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

// personSnapshot reads one person back after a command that should have decided them.
func personSnapshot(t *testing.T, f invitationSQLFixture, id uuid.UUID) *user.User {
	t.Helper()
	var got *user.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = f.Users.Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return got
}
