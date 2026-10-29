package internal_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// invitationMailer answers the one question a mailed credential has to survive:
// is the row its link names already committed, and therefore spendable by the
// person who opens the mail as soon as it lands? It reads through the owner's own
// connection, which is a second transaction, because inside the delivery's own
// transaction the row would be its own uncommitted write — and that is exactly
// the reading a recipient is not.
type invitationMailer struct {
	admin   *sql.DB
	token   string
	visible bool
	err     error
}

func (m *invitationMailer) Send(ctx context.Context, message notificationcontracts.Message) error {
	m.token = authtest.TokenIn(message.Body)
	if m.token == "" {
		return nil
	}
	m.err = m.admin.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM password_tokens WHERE token_hash = $1)",
		contracts.Hash(m.token)).Scan(&m.visible)
	return nil
}

// inviteThenCommit invites somebody in a transaction that ends, which is the
// ordering every real invitation has: the event a worker reads is relayed from
// committed rows, so the person a link is offered to exists for every reader
// before the mail is ever considered.
func inviteThenCommit(t *testing.T, conn *db.Conn, email string) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := realUsers().Invite(ctx, tx, email, "Invited")
		return err
	})
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
}

func TestInvitationMailCarriesACommittedCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &invitationMailer{admin: admin}
	mountConfigured(t, conn, auth.OIDC{}, false, func(deps *auth.Deps) { deps.Mailer = mailer })

	const address = "committed-invitation@example.com"
	inviteThenCommit(t, conn, address)
	worker(t, conn)

	if mailer.token == "" {
		t.Fatal("the worker sent no set-password credential")
	}
	if mailer.err != nil {
		t.Fatalf("read the credential from a separate connection: %v", mailer.err)
	}
	if !mailer.visible {
		t.Error("the mailer received the link before its credential was visible to another transaction")
	}
	var sent bool
	if err := admin.QueryRowContext(t.Context(),
		"SELECT sent_at IS NOT NULL FROM password_tokens WHERE token_hash = $1",
		contracts.Hash(mailer.token)).Scan(&sent); err != nil {
		t.Fatalf("read the delivery mark: %v", err)
	}
	if !sent {
		t.Error("a link the transport took is not recorded as a delivery, so an event that fails afterwards mails a second one")
	}
}

type interruptedInvitationMailer struct {
	interrupt bool
	attempts  int
	box       authtest.Mailbox
}

func (m *interruptedInvitationMailer) Send(ctx context.Context, message notificationcontracts.Message) error {
	m.attempts++
	if m.interrupt {
		m.interrupt = false
		panic("mail transport stopped before accepting the message")
	}
	return m.box.Send(ctx, message)
}

// TestInterruptedInvitationDeliverySendsLinkOnReplay is the other half of
// TestInvitationMailCarriesACommittedCredential. A credential committed ahead of
// its message must not become the link nobody was ever sent: the five-minute
// resend cooldown counts the deliveries that went out, so an offer whose mail
// never reached the transport is offered again.
func TestInterruptedInvitationDeliverySendsLinkOnReplay(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &interruptedInvitationMailer{interrupt: true}
	mountConfigured(t, conn, auth.OIDC{}, false, func(deps *auth.Deps) { deps.Mailer = mailer })

	const address = "interrupted-invitation@example.com"
	inviteThenCommit(t, conn, address)

	var event events.Event
	event.Name, event.TenantID = usercontracts.EventInvited, acme.ID
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", event.Name).
			Row().Scan(&event.ID, &event.Payload)
	}); err != nil {
		t.Fatalf("read the invitation event through its tenant: %v", err)
	}
	var handle events.Handler
	for _, sub := range subs {
		if sub.Name == event.Name {
			handle = sub.Handler
			break
		}
	}
	if handle == nil {
		t.Fatal("the invitation event has no set-password subscriber")
	}
	deliver := func() error {
		return db.Run(deliveryContext(t.Context(), conn, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return handle(ctx, tx, event)
		})
	}

	// An interrupted process closes its event transaction without acknowledging
	// the event. The credential's own transaction committed, and nothing was
	// mailed, so nothing may claim that a link went out.
	func() {
		defer func() { _ = recover() }()
		if err := deliver(); err != nil && mailer.attempts == 0 {
			t.Fatalf("first delivery never reached mail transport: %v", err)
		}
	}()
	var pending int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1", usercontracts.EventInvited).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if mailer.attempts != 1 || pending != 1 || len(mailer.box.Sent()) != 0 {
		t.Fatalf("interrupted delivery attempts=%d pending=%d messages=%d, want 1, 1, 0",
			mailer.attempts, pending, len(mailer.box.Sent()))
	}

	if err := deliver(); err != nil {
		t.Fatalf("replay the same invitation event: %v", err)
	}
	if got := len(mailer.box.Sent()); got != 1 {
		t.Fatalf("replayed invitation delivered %d set-password links, want 1", got)
	}
	// One link, and it is the one the table holds: no mail in this mailbox names
	// a credential the rotation already replaced.
	var live int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM password_tokens WHERE sent_at IS NOT NULL").Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Errorf("the table holds %d delivered credentials, want the one link this person was sent", live)
	}
}
