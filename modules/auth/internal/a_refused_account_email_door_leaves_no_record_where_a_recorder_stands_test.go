package internal_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestARefusedAccountEmailDoorLeavesNoRecordWhereARecorderStands asks the doors
// that promise an account email of the deployment that names no mail host and does
// keep a ledger. The kernel Needs a mail ledger of every auth composition, so a
// deployment that cannot mail is a deployment that records: the refusal has to
// leave that table as it found it, not fill it with the suppressed send the
// recorder exists to tempt a caller into writing. Each door is refused before its
// command runs, so an account, a token, an outbox row and a record of a mail are
// four ways the same refusal could write an address where it wrote nothing.
func TestARefusedAccountEmailDoorLeavesNoRecordWhereARecorderStands(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mails = mailLedger()
		undeliverable(d)
	})
	before := countsByTable(t, conn)
	for _, door := range []struct{ path, body string }{
		{"/api/v1/public/auth/register", approvalBody(t, "nobody@example.com", nil)},
		{"/api/v1/public/auth/resend-verification", `{"email":"nobody@example.com"}`},
		{"/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`},
	} {
		res := call(t, router, "POST", door.path, door.body, sameSite)
		if res.Code != http.StatusServiceUnavailable ||
			!strings.Contains(res.Body.String(), authtest.NoMailDetail) {
			t.Fatalf("%s answered %d %q, want the refusal the deployment answers with",
				door.path, res.Code, res.Body.String())
		}
	}
	after := countsByTable(t, conn)
	for table, was := range before {
		if after[table] != was {
			t.Errorf("%s holds %d rows where it held %d before a refused request", table, after[table], was)
		}
	}
	if n := len(mailbox.Sent()); n != 0 {
		t.Errorf("a refused door handed %d messages to the carrier, want none", n)
	}
}
