package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestInsideTheCooldownOnlyAnAskIsAnsweredWithAMessage holds the two callers of one
// cooldown branch apart. A person at the resend form inside the cooldown is still
// handed the one message that says no link was sent, because the delivery door
// answers their call from it; a redelivery of the sign-up event in the same state
// is nobody asking, and leaves the mailbox and the held link as they were.
func TestInsideTheCooldownOnlyAnAskIsAnsweredWithAMessage(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	const address = "cooldown-asker@example.com"
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/register",
		approvalBody(t, address, nil)); res.Code != http.StatusAccepted {
		t.Fatalf("registration = %d, want 202: %s", res.Code, res.Body.String())
	}
	signUp := events.Event{Name: usercontracts.EventRegistrationUnverified, TenantID: acme.ID}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", signUp.Name).
			Row().Scan(&signUp.ID, &signUp.Payload)
	}); err != nil {
		t.Fatalf("read the registration event through its tenant: %v", err)
	}
	asked, err := json.Marshal(contracts.VerificationRequested{Email: address, At: db.Now()})
	if err != nil {
		t.Fatal(err)
	}
	resend := events.Event{ID: uuid.New(), Name: contracts.EventVerificationRequested, TenantID: acme.ID, Payload: asked}
	deliver := func(event events.Event) {
		t.Helper()
		for _, sub := range subs {
			if sub.Name != event.Name {
				continue
			}
			if err := db.Run(deliveryContext(t.Context(), conn, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return sub.Handler(ctx, tx, event)
			}); err != nil {
				t.Fatalf("deliver %s: %v", event.Name, err)
			}
			return
		}
		t.Fatalf("%s has no subscriber", event.Name)
	}

	deliver(signUp)
	if got := len(mailbox.Sent()); got != 1 || authtest.TokenIn(mailbox.Sent()[0].Body) == "" {
		t.Fatalf("the sign-up mailed %d messages, want the one carrying the link", got)
	}
	token := authtest.TokenIn(mailbox.Sent()[0].Body)

	deliver(resend)
	if got := len(mailbox.Sent()); got != 2 {
		t.Fatalf("a resend inside the cooldown handed the transport %d messages in all, want 2: the ask is answered", got)
	}
	if authtest.TokenIn(mailbox.Sent()[1].Body) != "" {
		t.Error("a resend inside the cooldown carried a second link")
	}

	deliver(signUp)
	if got := len(mailbox.Sent()); got != 2 {
		t.Errorf("a redelivered sign-up inside the cooldown handed the transport %d messages in all, want 2", got)
	}
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/verify-email",
		verificationBody(t, token)); res.Code != http.StatusOK {
		t.Errorf("the link the person holds stopped working: %d", res.Code)
	}
}
