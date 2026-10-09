package internal_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestASetPasswordMailIsRecordedAndCarriesNoCredential is the brief's fourth
// decision applied to the mail it was never run against: the set-password link a
// forgotten-password request causes. The scan the branch already had ran for a
// verification mail only, and the two mails are minted by different code and
// mailed by different sends — `offer` and `offerVerification` — so one proving no
// secret escaped says nothing about the other.
//
// Both outcomes are scanned: the send the transport took, where the row says
// `sent` and the credential row exists, and the send it refused, where the row
// says `failed`, no credential survives the attempt, and the transport's own words
// are in the record only after RedactMailReason had them.
func TestASetPasswordMailIsRecordedAndCarriesNoCredential(t *testing.T) {
	for _, leg := range []struct {
		name string
		fail bool
	}{{"the transport took the link", false}, {"the transport refused it", true}} {
		t.Run(leg.name, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
			mailer := &verificationFailingMailer{fail: leg.fail}
			notice, _ := notificationmodule.New(notificationmodule.Deps{Mailer: notificationmodule.NewMailbox()})
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
				d.Mailer, d.Notify, d.Mails = mailer, notice, notice
			})
			person(t, conn, "ada@acme.localhost", contracts.RoleMember)

			res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"ada@acme.localhost"}`)
			if res.Code != http.StatusOK {
				t.Fatalf("forgot=%d %s, want 200", res.Code, res.Body.String())
			}
			worker(t, conn)
			if mailer.token == "" {
				t.Fatal("the transport was handed no link, so the scan below would prove nothing")
			}

			want := notification.MailSent
			if leg.fail {
				want = notification.MailFailed
			}
			outcome, reason, rows := outcomeOf(t, conn, contracts.MailSetPassword)
			if rows != 1 || outcome != want {
				t.Fatalf("set-password records=%d outcome=%q, want the one %q row", rows, outcome, want)
			}
			if !leg.fail && reason != "" {
				t.Errorf("a sent mail's record says %q, want nothing to say", reason)
			}
			if leg.fail {
				if reason == "" {
					t.Error("a refused mail's record says nothing about why")
				}
				if strings.Contains(reason, mailer.token) || len(reason) > notification.MaxMailReason {
					t.Errorf("the failed row's reason carries the secret or is unbounded: %q", reason)
				}
			}

			// What the transaction that wrote the record was allowed to keep: the
			// credential it mailed on a send that left, none on a send that did not.
			var tokens int
			if err := admin.QueryRow("SELECT count(*) FROM password_tokens").Scan(&tokens); err != nil {
				t.Fatal(err)
			}
			if want == notification.MailSent && tokens != 1 {
				t.Errorf("password_tokens=%d beside a sent mail, want the one link the person was sent", tokens)
			}
			if want == notification.MailFailed && tokens != 0 {
				t.Errorf("password_tokens=%d beside a refused mail, want none: a committed failure may hold no credential", tokens)
			}

			// And the whole schema, enumerated from the database rather than named
			// here, holds the link nowhere. Its SHA-256 is deliberately not searched
			// for: password_tokens and verification_tokens exist to hold exactly that
			// digest (the case above reads their counts), and a text rendering of a
			// bytea column shows it as hex, so scanning for it would report the
			// credential store doing its job. What must not be stored is the
			// bearer — the same line the neighbouring cases hold.
			refuseNothingYet(t, admin, mailer.token)
		})
	}
}
