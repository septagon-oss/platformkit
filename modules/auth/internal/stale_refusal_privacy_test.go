package internal_test

import (
	"context"
	"errors"
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

// refusingOneMailbox is a transport that refuses one recipient and takes every
// other: a relay that rejects a mailbox at RCPT while the rest of the mail flows.
type refusingOneMailbox struct {
	refused string
	box     notification.Mailer
}

func (m refusingOneMailbox) Send(ctx context.Context, message notification.Message) error {
	if message.To == m.refused {
		return errors.New("550 mailbox unavailable")
	}
	return m.box.Send(ctx, message)
}

// A refusal of somebody else's mail, still the tenant's newest record, must not
// let a stranger tell an address with an account from one without: while the
// transport takes mail, the probed address's answer is the same either way.
func TestAnEarlierRefusalDoesNotDiscloseWhetherAnAccountExists(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := refusingOneMailbox{refused: "refused@acme.localhost", box: notificationmodule.NewMailbox()}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	person(t, conn, "refused@acme.localhost", contracts.RoleMember)
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	forgot := func(email string) string {
		t.Helper()
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"`+email+`"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("forgot %s status=%d, want 200", email, res.Code)
		}
		return res.Header().Get("X-Request-ID")
	}
	// The same state behind every probe: a refusal, established through the
	// private ledger rather than through the public answer under test, is the
	// tenant's newest record when the probed address is asked for.
	probe := func(email string) string {
		t.Helper()
		forgot("refused@acme.localhost")
		relayWithRequest(t, conn)
		if outcome, _, _ := outcomeOf(t, conn, contracts.MailSetPassword); outcome != notification.MailFailed {
			t.Fatalf("newest delivery outcome=%q before probing %s, want the refused attempt", outcome, email)
		}
		id := forgot(email)
		relayWithRequest(t, conn)
		return ask(router, id)
	}
	known := probe("ada@acme.localhost")
	if outcome, _, _ := outcomeOf(t, conn, contracts.MailSetPassword); outcome != notification.MailSent {
		t.Fatalf("newest delivery outcome=%q after probing the account, want its mail sent", outcome)
	}
	unknown := probe("nobody@acme.localhost")
	if known != unknown {
		t.Errorf("anonymous delivery answers disclose account existence after an earlier refusal: known=%q unknown=%q; want identical answers", known, unknown)
	}
}
