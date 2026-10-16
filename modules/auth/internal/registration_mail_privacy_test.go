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

func TestFailedRegistrationMailDoesNotDiscloseAnExistingAccount(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	person(t, conn, "taken@acme.localhost", contracts.RoleMember)
	register := func(email string) string {
		t.Helper()
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/register", approvalBody(t, email, nil))
		if res.Code != http.StatusAccepted {
			t.Fatalf("register %s status=%d, want 202: %s", email, res.Code, res.Body.String())
		}
		id := res.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("registration response omitted its request id")
		}
		return id
	}
	taken, fresh := register("taken@acme.localhost"), register("fresh@acme.localhost")
	relayWithRequest(t, conn)
	// Reach the assertion through the private record: the worker attempted the
	// fresh account's mail and the transport refused it. The public answer is
	// deliberately not a precondition for testing the door's neutrality.
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailVerification); rows == 0 || outcome != notification.MailFailed {
		t.Fatalf("verification rows=%d outcome=%q, want a recorded transport failure", rows, outcome)
	}
	a, b := ask(router, taken), ask(router, fresh)
	if a != b {
		t.Errorf("anonymous registration delivery answers disclose account existence during SMTP failure: taken=%q fresh=%q; want identical answers", a, b)
	}
}
