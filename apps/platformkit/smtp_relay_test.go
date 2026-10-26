package main

import (
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail/gomailtest"
)

// mailing remembers which installation each case asked for, the same way `copied`
// does: writeConfig reads it, so a case that names a mail host does not move the
// 122 cases that need the recording carrier exactly as it is.
var mailing sync.Map // *testing.T → *gomailtest.Relay

// mailSection is the config this installation reads as "a mail host is configured",
// which is the same three keys the composition's own pick needs non-empty and the
// relay answers without a credential.
func mailSection(r *gomailtest.Relay) string {
	return "mail:\n  host: \"" + r.Host() + "\"\n  port: " + strconv.Itoa(r.Port()) +
		"\n  from: \"platformkit@acme.localhost\"\n"
}

// configureMailing is configure in the installation that named a mail host: the
// relay on loopback is this deployment's SMTP server, and what it accepted is where
// a case reads the link the composition mailed.
func configureMailing(t *testing.T) (string, config.Config, *gomailtest.Relay) {
	t.Helper()
	relay := gomailtest.Start(t)
	mailing.Store(t, relay)
	t.Cleanup(func() { mailing.Delete(t) })
	path, cfg := configure(t)
	return path, cfg, relay
}

// startedMailing is the same installation with its mail host named: booted, with
// the installation's own administrator signed in, mailing to the relay.
func startedMailing(t *testing.T) (config.Config, *http.Client, *gomailtest.Relay) {
	t.Helper()
	path, cfg, relay := configureMailing(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	return cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass), relay
}
