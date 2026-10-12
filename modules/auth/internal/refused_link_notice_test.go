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

// TestARefusedSetPasswordMailLeavesNoNoticeThatSaysItWasSent is the brief's second
// decision held by the in-app notice: "we sent you a link" is said only when a
// record says `sent`.
//
// offer raises a notice — "The link is in the email this raised" — before it
// hands the mail to the transport. A refused send used to roll that notice back
// with the attempt; it is now acknowledged so its failed record commits, and the
// notice must not commit beside a record that says the mail did not go.
func TestARefusedSetPasswordMailLeavesNoNoticeThatSaysItWasSent(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	notices, _ := notificationmodule.New(notificationmodule.Deps{Mailer: notificationmodule.NewMailbox()})
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.Mailer, d.Notify, d.Mails = mailer, notices, notices
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)

	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"ada@acme.localhost"}`); res.Code != http.StatusOK {
		t.Fatalf("forgot=%d %s, want 200", res.Code, res.Body.String())
	}
	worker(t, conn)
	if mailer.token == "" {
		t.Fatal("the transport was never handed a link, so nothing below was reached")
	}
	outcome, _, rows := outcomeOf(t, conn, contracts.MailSetPassword)
	if rows != 1 || outcome != notification.MailFailed {
		t.Fatalf("set-password records=%d outcome=%q, want the one failed row", rows, outcome)
	}
	var said int
	if err := admin.QueryRow("SELECT count(*) FROM notifications").Scan(&said); err != nil {
		t.Fatal(err)
	}
	if said != 0 {
		t.Errorf("notifications=%d beside a record that says the mail failed, want none: a committed notice"+
			" tells the person the link is in an email that never left", said)
	}
}
