package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

func TestFailedMailDoesNotDiscloseWhetherAnAccountExists(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	request := func(email string) string {
		t.Helper()
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"`+email+`"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("forgot status=%d, want 200", res.Code)
		}
		id := res.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("forgot response omitted its request id")
		}
		return id
	}
	known, unknown := request("ada@acme.localhost"), request("nobody@acme.localhost")
	relayWithRequest(t, conn)
	// Establish the transport failure through the tenant's private ledger, not
	// through the public answer whose privacy is under test.
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailSetPassword); rows != 1 || outcome != notification.MailFailed {
		t.Fatalf("delivery rows=%d outcome=%q, want one failed attempt", rows, outcome)
	}
	a, b := ask(router, known), ask(router, unknown)
	if a != b {
		t.Errorf("anonymous delivery answers disclose account existence during an SMTP failure: known=%q unknown=%q; want identical answers", a, b)
	}
}
