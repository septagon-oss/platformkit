package internal_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestTwoResetRequestsAtOnceMailOneLink is the reason offer takes passwordLock
// before recent(): two workers that both read "no link yet" would both mail. Two
// forgotten-password requests for one person are delivered at the same moment, in
// two transactions; one link leaves, one row holds it, and the record says one
// mail was sent.
func TestTwoResetRequestsAtOnceMailOneLink(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mails = mailLedger() })
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	for range 2 {
		if res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"ada@acme.localhost"}`); res.Code != http.StatusOK {
			t.Fatalf("forgot=%d %s", res.Code, res.Body.String())
		}
	}
	var payloads [][]byte
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT payload FROM platformkit_outbox WHERE name = ? ORDER BY created_at, id",
			contracts.EventResetRequested).Scan(&payloads).Error
	}); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 {
		t.Fatalf("reset requests in the outbox=%d, want the two that were asked", len(payloads))
	}
	var handle events.Subscription
	for _, s := range subs {
		if s.Name == contracts.EventResetRequested {
			handle = s
		}
	}
	if handle.Handler == nil {
		t.Fatal("no reset subscription to deliver to")
	}

	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, payload := range payloads {
		workers.Go(func() {
			<-start
			err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return handle.Handler(ctx, tx, events.Event{ID: uuid.New(), Name: handle.Name, TenantID: acme.ID, Payload: payload})
			})
			if err != nil {
				t.Errorf("deliver a reset request: %v", err)
			}
		})
	}
	close(start)
	workers.Wait()

	if got := len(mailbox.Sent()); got != 1 {
		t.Errorf("links mailed=%d, want one: the second worker must read the first one's link", got)
	}
	var tokens int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens").Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 1 {
		t.Errorf("password tokens=%d, want the one that was mailed", tokens)
	}
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailSetPassword); rows != 1 || outcome != notification.MailSent {
		t.Errorf("set-password records=%d outcome=%q, want one sent row", rows, outcome)
	}
}
