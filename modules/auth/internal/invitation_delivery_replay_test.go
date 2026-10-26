package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestAcceptedInvitationSurvivesDeliveryRollbackAndReplay(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false)
	inviteThenCommit(t, conn, "delivery-replay@example.com")
	ctx := deliveryContext(t.Context(), conn, acme)
	var event events.Event
	event.Name, event.TenantID = usercontracts.EventInvited, acme.ID
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", event.Name).
			Row().Scan(&event.ID, &event.Payload)
	}); err != nil {
		t.Fatal(err)
	}
	var handle events.Handler
	for _, sub := range subs {
		if sub.Name == event.Name {
			handle = sub.Handler
			break
		}
	}
	if handle == nil {
		t.Fatal("invitation subscriber is absent")
	}
	// The transport is asked only once the delivery's own work commits, so a later
	// operation that prevents the event transaction from committing mails nobody
	// and leaves no credential behind. Its next delivery uses the same id.
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := handle(ctx, tx, event); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("delivery rollback = %v", err)
	}
	if got := len(mailbox.Sent()); got != 0 {
		t.Fatalf("messages mailed by a delivery that did not commit = %d, want none", got)
	}
	var credentials int
	row(t, admin, "SELECT count(*) FROM password_tokens").Scan(&credentials)
	if credentials != 0 {
		t.Errorf("a delivery that did not commit left %d credentials, want none", credentials)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return handle(ctx, tx, event)
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(mailbox.Sent()); got != 1 {
		t.Errorf("replayed accepted delivery sent %d messages, want one", got)
	}
	token := authtest.TokenIn(mailbox.Sent()[0].Body)
	if token == "" {
		t.Fatal("the delivery that committed mailed no credential")
	}
	// A different tenant cannot consume the independently committed credential.
	err = db.Run(deliveryContext(t.Context(), conn, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
		if session != nil {
			t.Error("another tenant received a session")
		}
		return err
	})
	if !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("another tenant's reset = %v, want ErrCredentials", err)
	}
	// Two readers spend the original mailed link: exactly one commits, and the
	// loser returns neither a session nor another password-reset event.
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
				if err != nil && session != nil {
					t.Error("refused reset returned a session")
				}
				return err
			})
		}()
	}
	won, refused := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			won++
		case errors.Is(err, contracts.ErrCredentials):
			refused++
		default:
			t.Errorf("reset = %v", err)
		}
	}
	if won != 1 || refused != 1 {
		t.Errorf("concurrent resets: %d committed, %d refused; want one each", won, refused)
	}
	var emitted int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1",
		contracts.EventPasswordReset).Scan(&emitted); err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Errorf("password reset events = %d, want one", emitted)
	}
}
