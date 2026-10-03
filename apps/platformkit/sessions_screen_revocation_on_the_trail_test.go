package main

// The sessions screen on the operator's trail. The screen's
// "End the other N" button posts to /app/auth/sessions/revoke-rest, which
// answers through the auth module's except-taking revocation — the command the
// module's own contract keeps silent, because it was written for the password
// change, where "the revocation is a clause of the password change, not a
// decision anybody made on its own". On the screen that clause is inverted:
// the person decides on its own, and nothing else in the transaction publishes
// a word. Pressing it signs another machine out and leaves no revocation in
// the outbox, so `modules/audit` has nothing to copy and the trail the pillar
// contract promises — every state change readable afterwards — holds no record
// of the decision. The JSON route beside it (`auth-session-revoke-all`)
// publishes one auth.session_revoked per removed row; the screen does not, for
// the same fact. The assertions reach the trail and the state through the
// session's own key and a count — never through a sentence the refusal prints.

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestEndingTheOtherSessionsFromTheScreenIsOnTheTrail(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	here := signIn(t, cfg, acmeHost, adminEmail, adminPass)  // the session reading the page
	there := signIn(t, cfg, acmeHost, adminEmail, adminPass) // another machine, to be ended

	// A client that does not follow redirects: the 303 is an answer under test,
	// and following it lands on a 200 whatever it says.
	asking := &http.Client{Jar: here.Jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	if code, body := do(t, cfg, asking, http.MethodGet, acmeHost, "/app/auth/sessions", ""); code != http.StatusOK {
		t.Fatalf("the sessions screen at the composition = %d %s, want 200 to a signed-in person", code, body)
	}
	code, body := do(t, cfg, asking, http.MethodPost, acmeHost, "/app/auth/sessions/revoke-rest", "")
	if code != http.StatusSeeOther {
		t.Fatalf("ending every session but this one = %d %s, want 303", code, body)
	}

	// The state first: the other machine is gone. Asserted as a status for that
	// session's own credential — never through what the broken trail prints.
	if code, _ := do(t, cfg, there, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code == http.StatusOK {
		t.Fatal("the other session still answers /auth/me after the screen ended every session but one")
	}

	// And the trail: the revocation the person decided is one readable record
	// for the session that left. This is the assertion that fails today.
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	eventually(t, "the sessions screen's revocation in the audit trail, one row for the session it ended", func() bool {
		var rows int
		err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM audit_events WHERE name = $1`, authcontracts.EventSessionRevoked).Scan(&rows)
		return err == nil && rows == 1
	})
}
