package internal_test

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// knownAsks are the three public doors whose acknowledgment promises this person
// an email, asked about an address somebody really has. A refusal that only ever
// ran against an address nobody has would not be the deployment's own fact but a
// side effect of the lookup, and the case that pins it would pass for the wrong
// reason.
func knownAsks(t *testing.T, router http.Handler, email string) map[string]struct {
	status int
	detail string
} {
	t.Helper()
	bodies := map[string]string{
		"/api/v1/public/auth/register":            approvalBody(t, email, nil),
		"/api/v1/public/auth/resend-verification": `{"email":"` + email + `"}`,
		"/api/v1/public/auth/password/forgot":     `{"email":"` + email + `"}`,
	}
	out := map[string]struct {
		status int
		detail string
	}{}
	for path, body := range bodies {
		res := call(t, router, http.MethodPost, path, body, fromThePageItself)
		var problem struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &problem)
		out[path] = struct {
			status int
			detail string
		}{res.Code, problem.Detail}
	}
	return out
}

// fromThePageItself is the origin these public doors insist on before they read a
// body: the account request has to come from the site that asked for it.
func fromThePageItself(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }

// TestAMailLessDoorRefusesAnAddressSomebodyHasUntilTheInstallationCanMail stands
// in the deployment that named no mail host and asks each account-email door about
// an address somebody has — an account the case created through the user module —
// rather than the address nobody has that the neutrality rules usually ask about.
//
// It pins three things the invariant needs and a door that answers only unknown
// addresses would not: the refusal is the same for a known address as for an
// unknown one, down to the status and the sentence; the refused call writes no row
// in the four tables it could touch, publishes none of the four events it could
// raise and hands nothing to the carrier; and the refusal is the installation's
// answer and not the request's — the same three doors, the same address, asked once
// more after the carrier says it dials, are answered the way an installation that
// can mail answers them, with the account, the token and the event that request
// owes.
func TestAMailLessDoorRefusesAnAddressSomebodyHasUntilTheInstallationCanMail(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	// The carrier answers about itself at the moment it is asked, the way
	// authtest.Mailbox answers through its own field: nothing here is booted twice,
	// so the two answers below come from one installation that changed its mind
	// about its relay rather than from two fixtures.
	var delivers atomic.Bool
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.MailDelivery = notificationcontracts.MailDeliveryFunc(delivers.Load)
	})
	const known = "someone@acme.localhost"
	person(t, conn, known)
	if delivers.Load() {
		t.Fatal("this installation starts with no mail host")
	}

	before := countsByTable(t, conn)
	asked := knownAsks(t, router, known)
	for path, answer := range asked {
		if answer.status != http.StatusServiceUnavailable {
			t.Errorf("%s answered %d about an address somebody has, want the unavailable refusal", path, answer.status)
		}
		if answer.detail != authtest.NoMailDetail {
			t.Errorf("%s said %q about an address somebody has, want the deployment's own unavailable sentence", path, answer.detail)
		}
	}
	// The same three doors, an address nobody has: one answer, so the refusal says
	// nothing about who holds an account.
	for path, answer := range knownAsks(t, router, "nobody@acme.localhost") {
		if answer.status != asked[path].status || answer.detail != asked[path].detail {
			t.Errorf("%s answered an address nobody has %d %q where it answered an address somebody has %d %q",
				path, answer.status, answer.detail, asked[path].status, asked[path].detail)
		}
	}
	for table, was := range before {
		if now := countsByTable(t, conn)[table]; now != was {
			t.Errorf("%s holds %d rows where it held %d before these refused requests", table, now, was)
		}
	}
	if n := eventsPublished(t, conn); n != 0 {
		t.Errorf("refused account emails published %d events, want none", n)
	}
	if n := len(mailbox.Sent()); n != 0 {
		t.Errorf("refused account emails handed %d messages to the carrier, want none", n)
	}

	// The installation gets a mail server. The very same doors, the same address,
	// now owe the account, the token and the event their acknowledgment promises.
	delivers.Store(true)
	accepted := knownAsks(t, router, known)
	for path, answer := range accepted {
		if answer.status == http.StatusServiceUnavailable {
			t.Errorf("%s still answers %q where the carrier says it dials", path, answer.detail)
			continue
		}
		if answer.status != http.StatusAccepted && answer.status != http.StatusOK {
			t.Errorf("%s answered %d where this installation can mail, want the acceptance it honours", path, answer.status)
		}
	}
	// The events are there before the worker reads them: each accepted request
	// committed its own row in the transaction it was answered from. Once the
	// worker has consumed them the outbox holds what is still waiting, so this is
	// the moment to ask whether an accepted request published at all.
	if n := eventsPublished(t, conn); n == 0 {
		t.Error("an accepted account email published none of the events it promises")
	}
	worker(t, conn)
	after := countsByTable(t, conn)
	if after["password_tokens"] <= before["password_tokens"] {
		t.Errorf("the forgotten-password request wrote no token: %d rows where it held %d",
			after["password_tokens"], before["password_tokens"])
	}
	if len(mailbox.Sent()) == 0 {
		t.Error("an accepted account email handed nothing to a carrier that says it dials")
	}
}
