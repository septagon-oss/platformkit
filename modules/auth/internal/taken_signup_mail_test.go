package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestASignUpForATakenAddressMailsOneMessageAndNoCredential pins the sign-up
// route's taken-address branch while the transport takes every mail: that call
// hands the transport one message of its own, the message carries no link, its
// record says sent, and the door answers it exactly as it answers a sign-up that
// made an account.
func TestASignUpForATakenAddressMailsOneMessageAndNoCredential(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{}
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
		return res.Header().Get("X-Request-ID")
	}
	taken, fresh := register("taken@acme.localhost"), register("fresh@acme.localhost")
	relayWithRequest(t, conn)

	to := map[string][]string{}
	for _, m := range mailer.box.Sent() {
		to[m.To] = append(to[m.To], authtest.TokenIn(m.Body))
	}
	if got := to["taken@acme.localhost"]; len(got) != 1 || got[0] != "" {
		t.Errorf("taken address was mailed %d messages with tokens %q, want one message and no link", len(got), got)
	}
	if got := to["fresh@acme.localhost"]; len(got) != 1 || got[0] == "" {
		t.Errorf("fresh address was mailed %d messages with tokens %q, want one verification link", len(got), got)
	}
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailVerificationNoLink); rows != 1 || outcome != notification.MailSent {
		t.Errorf("no-link records=%d outcome=%q, want one sent record for the taken address", rows, outcome)
	}
	if a, b := ask(router, taken), ask(router, fresh); a != b || a != "200 "+notification.MailStatePending {
		t.Errorf("door answered taken=%q fresh=%q, want both %q", a, b, "200 "+notification.MailStatePending)
	}
}
