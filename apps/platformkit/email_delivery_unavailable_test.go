package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestEmailRegistrationRefusesWithoutDeliverableMail boots the state an
// installation is actually in when it has no relay: a production deployment that
// names no mail server composes the sender that refuses every send
// (modules/notification's "none"), and the command whose promise is the message
// itself answers a reasoned 503 and writes nothing. The same configuration in
// development is a different deployment and gets the module's simulated mailbox —
// mail_transport_test.go pins that other half.
func TestEmailRegistrationRefusesWithoutDeliverableMail(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	if cfg.Mail.Enabled() {
		t.Fatal("the test requires the example configuration's empty mail host")
	}
	c := composeReference(cfg, pkit.Production).composition
	opts := appOptions(cfg, c, app.All)
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)

	const address = "undeliverable@acme.localhost"
	const password = "correct horse battery staple"
	body := `{"email":"` + address + `","displayName":"Undeliverable","password":"` + password + `","confirmation":"` + password + `","termsAccepted":true}`
	status, answer := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", body)
	if status != http.StatusServiceUnavailable || !strings.Contains(strings.ToLower(answer), "mail") {
		t.Errorf("registration with no deliverable mail = %d %s, want a reasoned 503", status, answer)
	}

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var users, events int
	if err := owner.QueryRowContext(t.Context(), "SELECT count(*) FROM users WHERE email = $1", address).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'user.registration_unverified' AND payload->>'email' = $1", address).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if users != 0 || events != 0 {
		t.Errorf("refused registration left %d user rows and %d registration events; want neither", users, events)
	}
}
