package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestTheMailLedgerIsNotAskedWhetherAMailMayBePromised separates two answers: the
// ledger says whether a send left a record, and Delivery.deliversAccountMail says
// whether this deployment may promise a mail, from a sender, a host and the carrier's
// own account of itself. The forgot door is asked of a composition whose carrier dials
// — with a recorder wired, and with nothing wired to record — and answers both times
// without refusing: a recorder read as availability turns one forgotten row into a refusal.
func TestTheMailLedgerIsNotAskedWhetherAMailMayBePromised(t *testing.T) {
	for name, wire := range map[string]func(*auth.Deps){
		"a recorder it can write to":       func(d *auth.Deps) { d.Mails = mailLedger() },
		"nothing wired to record the send": func(d *auth.Deps) { d.Mails, d.MailDelivery = nil, authtest.Delivers },
	} {
		_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
		router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, wire)
		res := call(t, router, "POST", "/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`, sameSite)
		if res.Code >= http.StatusInternalServerError {
			t.Errorf("the forgot door answered %d with %s: the ledger records a send, it does not answer whether one may be promised", res.Code, name)
		}
	}
}
