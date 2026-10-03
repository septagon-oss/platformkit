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
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestAcceptedVerificationDeliveryIsNotMailedAgainOnReplay(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/register",
		approvalBody(t, "one-delivery@example.com", nil)); res.Code != http.StatusAccepted {
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
	for range 2 {
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return handle(ctx, tx, event)
		}); err != nil {
			t.Fatalf("deliver registration event %s: %v", event.ID, err)
		}
	}
	if got := len(mailbox.Sent()); got != 1 {
		t.Fatalf("replayed accepted delivery sent %d links, want one", got)
	}
	token := authtest.TokenIn(mailbox.Sent()[0].Body)
	if token == "" {
		t.Fatal("accepted delivery had no verification link")
	}
	var delivered bool
	if err := admin.QueryRowContext(t.Context(),
		"SELECT sent_at IS NOT NULL FROM verification_tokens WHERE tenant_id = $1", acme.ID).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Error("accepted delivery did not record when it was sent")
	}
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/verify-email",
		verificationBody(t, token)); res.Code != http.StatusOK {
		t.Errorf("the one emailed link became invalid on replay: %d", res.Code)
	}
}
