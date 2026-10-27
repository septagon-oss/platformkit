package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A handoff the transport refused after a resend leaves one invitation holding no
// live credential at all, and the trail says so. That state is recoverable only if
// nothing the refusal touched keeps the next handoff from working, so this asks
// whether a further resend, with the transport healthy again, still mails a link
// that commits the membership — and whether the refusal stays legible afterwards:
// one failed row between two sent ones, the refusal the only row saying why, and
// only the one failure event the refusal committed with. A build that consumed the
// delivery identity on the refusal, or that recorded the recovery as a second
// refusal, satisfies every case beside this one.
func TestAResendAfterARefusedHandoffMailsAWorkingLinkAndKeepsTheTrailLegible(t *testing.T) {
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
	fresh := authtest.InvitationOffer(t, f.InvitationFixture, invitationRow(t, f, u.ID))
	if fresh == "" {
		t.Fatal("the resend after a refused handoff mailed no link, so the door claim below proves nothing")
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Auth.Reset(ctx, tx, fresh, "a sufficiently strong password", contracts.Client{})
		return err
	}); err != nil {
		t.Fatalf("the link the later resend mailed returned %v, want the recovered invitation to open the door", err)
	}

	var rows, taken, declined, reasons, events int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT count(*),
			count(*) FILTER (WHERE outcome = $2), count(*) FILTER (WHERE outcome = $3),
			count(*) FILTER (WHERE reason <> '')
			FROM direct_mail_deliveries WHERE kind = $1`,
			contracts.MailSetPassword, notification.MailSent, notification.MailFailed).
			Row().Scan(&rows, &taken, &declined, &reasons); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = $1", contracts.EventMailFailed).
			Row().Scan(&events)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 3 || taken != 2 || declined != 1 {
		t.Errorf("%d records, %d taken and %d refused, want a sent row per handoff the transport took and one failed row for the refusal",
			rows, taken, declined)
	}
	if reasons != 1 {
		t.Errorf("%d of the %d records name a reason, want the refusal alone to say why, even after the recovery", reasons, rows)
	}
	if events != 1 {
		t.Errorf("%d mail-failure events, want the one the refusal committed with and none beside the recovered send", events)
	}
	if got := invitationRow(t, f, u.ID); got.Status != user.StatusActive || got.Invitation.Phase != user.InvitationAccepted {
		t.Errorf("the recovered invitation reads status %q phase %q, want the accepted membership that link carried",
			got.Status, got.Invitation.Phase)
	}
}

// resendInvitation rotates the generation at the person's current revision, and
// fails the case when the command refuses, because every claim below is about the
// invitation the resend left behind.
func resendInvitation(t *testing.T, f invitationSQLFixture, id uuid.UUID, revision int64) {
	t.Helper()
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, id, revision)
		return err
	}); err != nil {
		t.Fatalf("the resend at revision %d returned %v, want the current revision accepted", revision, err)
	}
}

// invitationRow is what one transaction sees of one person: the generation, revision
// and delivery identity a handoff needs are all written by the command before it.
func invitationRow(t *testing.T, f invitationSQLFixture, id uuid.UUID) *user.User {
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
