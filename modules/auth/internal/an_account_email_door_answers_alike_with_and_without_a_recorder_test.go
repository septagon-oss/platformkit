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

// TestAnAccountEmailDoorAnswersTheSameWithAndWithoutAMailRecorder asks each door
// that promises an account email — sign-up, resend verification, forgotten
// password — of two installations whose carrier says it dials and that differ in
// one wiring: whether anything is there to record the send. The answer must be the
// same, and it must be the door's acceptance.
//
// A mail ledger answers what became of a mail, not whether one may be promised, so
// the presence of a recorder is nothing a door weighs before it refuses. Reading it
// as availability would turn a deployment that forgot to wire a recorder into one
// that refuses to sign anybody up; a door that answers 404 or 400 where it used to
// accept is the same accident one release later, and an answer that merely stays
// short of a server error would let both through.
func TestAnAccountEmailDoorAnswersTheSameWithAndWithoutAMailRecorder(t *testing.T) {
	for _, door := range []struct{ path, body string }{
		{"/api/v1/public/auth/register", approvalBody(t, "nobody@example.com", nil)},
		{"/api/v1/public/auth/resend-verification", `{"email":"nobody@example.com"}`},
		{"/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`},
	} {
		answers := map[string]int{}
		for name, wire := range map[string]func(*auth.Deps){
			"a recorder is wired":  func(d *auth.Deps) { d.Mails, d.MailDelivery = mailLedger(), authtest.Delivers },
			"no recorder is wired": func(d *auth.Deps) { d.Mails, d.MailDelivery = nil, authtest.Delivers },
		} {
			_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, wire)
			res := call(t, router, "POST", door.path, door.body, sameSite)
			answers[name] = res.Code
			if res.Code >= http.StatusBadRequest {
				t.Errorf("%s answered %d with %s, want the acceptance it answers with a recorder wired",
					door.path, res.Code, name)
			}
		}
		if answers["a recorder is wired"] != answers["no recorder is wired"] {
			t.Errorf("%s answered %d with a recorder and %d without one: the ledger records a send, it does not answer whether one may be promised",
				door.path, answers["a recorder is wired"], answers["no recorder is wired"])
		}
	}
}
