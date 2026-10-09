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

func TestFailedVerificationResendDoesNotDiscloseAnActiveAccount(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	person(t, conn, "active@acme.localhost", contracts.RoleMember)
	resend := func(email string) string {
		t.Helper()
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/resend-verification", `{"email":"`+email+`"}`)
		if res.Code != http.StatusAccepted {
			t.Fatalf("resend %s status=%d, want 202: %s", email, res.Code, res.Body.String())
		}
		id := res.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("resend response omitted its request id")
		}
		return id
	}
	active, absent := resend("active@acme.localhost"), resend("absent@acme.localhost")
	relayWithRequest(t, conn)
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailVerificationNoLink); rows == 0 || outcome != notification.MailFailed {
		t.Fatalf("no-link verification rows=%d outcome=%q, want a recorded transport failure", rows, outcome)
	}
	a, b := ask(router, active), ask(router, absent)
	if a != b {
		t.Errorf("anonymous resend delivery answers disclose account existence during SMTP failure: active=%q absent=%q; want identical answers", a, b)
	}
}
