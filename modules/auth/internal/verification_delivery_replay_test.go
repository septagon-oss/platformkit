package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type interruptedVerificationMailer struct {
	interrupt bool
	attempts  int
	box       authtest.Mailbox
}

func (m *interruptedVerificationMailer) Send(ctx context.Context, message notificationcontracts.Message) error {
	m.attempts++
	if m.interrupt {
		m.interrupt = false
		panic("mail transport stopped before accepting the message")
	}
	return m.box.Send(ctx, message)
}

func TestInterruptedVerificationDeliverySendsLinkOnReplay(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &interruptedVerificationMailer{interrupt: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup,
		func(deps *auth.Deps) { deps.Mailer = mailer })
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/register",
		approvalBody(t, "interrupted-delivery@example.com", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("registration = %d, want 202: %s", res.Code, res.Body.String())
	}
	var event events.Event
	event.Name, event.TenantID = usercontracts.EventRegistrationUnverified, acme.ID
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", event.Name).
			Row().Scan(&event.ID, &event.Payload)
	}); err != nil {
		t.Fatalf("read the registration event through its tenant: %v", err)
	}
	var handle events.Handler
	for _, sub := range subs {
		if sub.Name == event.Name {
			handle = sub.Handler
			break
		}
	}
	if handle == nil {
		t.Fatal("the registration event has no verification subscriber")
	}
	deliver := func() error {
		return db.Run(deliveryContext(t.Context(), conn, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return handle(ctx, tx, event)
		})
	}

	// An interrupted process closes its event transaction without acknowledging
	// the event. The credential's separate transaction has already committed.
	func() {
		defer func() {
			_ = recover()
		}()
		err := deliver()
		if err != nil && mailer.attempts == 0 {
			t.Fatalf("first delivery never reached mail transport: %v", err)
		}
	}()

	var pending int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1",
		usercontracts.EventRegistrationUnverified).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if mailer.attempts != 1 || pending != 1 || len(mailer.box.Sent()) != 0 {
		t.Fatalf("interrupted delivery attempts=%d pending=%d messages=%d, want 1, 1, 0",
			mailer.attempts, pending, len(mailer.box.Sent()))
	}

	if err := deliver(); err != nil {
		t.Fatalf("replay the same registration event: %v", err)
	}
	if got := len(mailer.box.Sent()); got != 1 {
		t.Errorf("replayed registration delivered %d verification links, want 1", got)
	}
}
