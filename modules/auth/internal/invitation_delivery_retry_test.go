package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

// The normal mailbox supplies successful delivery; this decorator refuses only
// the first attempt, as a mail transport with a transient outage would.
type recoveringInvitationMailer struct {
	box      authtest.Mailbox
	attempts int
}

func (m *recoveringInvitationMailer) Send(ctx context.Context, msg notificationcontracts.Message) error {
	m.attempts++
	if m.attempts == 1 {
		return errors.New("mail transport temporarily unavailable")
	}
	return m.box.Send(ctx, msg)
}

func TestAnInvitationRetriesAfterTheMailTransportRecovers(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	mailer := &recoveringInvitationMailer{}
	_, manifest := auth.Module(auth.Deps{
		Users: users, Mailer: mailer, Hosts: authtest.Host("acme.example.com"),
	})
	seed(t, conn, acme)
	// This is only a deadlock watchdog. Relay completion, not elapsed time,
	// establishes that the transport has finished its delivery attempts.
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	transport := memory.New()
	if err := events.Consume(ctx, conn, transport, manifest.Subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := users.Invite(ctx, tx, "invited@acme.example.com", "Invited")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.Relay(ctx, conn, transport); err != nil {
		t.Fatalf("relay invitation: %v", err)
	}
	var handled, dead, pending int
	row(t, admin, `SELECT count(*) FROM platformkit_handled`).Scan(&handled)
	row(t, admin, `SELECT count(*) FROM platformkit_dead_letters`).Scan(&dead)
	row(t, admin, `SELECT count(*) FROM platformkit_outbox WHERE published_at IS NULL`).Scan(&pending)
	if sent := len(mailer.box.Sent()); sent != 1 {
		t.Fatalf("relay completed with %d mails after %d send attempts; handled=%d dead=%d pending=%d; want one delivered invitation after recovery",
			sent, mailer.attempts, handled, dead, pending)
	}
	if err := events.Relay(ctx, conn, transport); err != nil {
		t.Fatal(err)
	}
	if sent := len(mailer.box.Sent()); sent != 1 {
		t.Errorf("another relay delivered %d total mails, want one", sent)
	}
}
