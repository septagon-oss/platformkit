package main

import (
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestAMailedLinkCarriesTheAddressTheTenantIsServedAt is the brief's fourth
// decided rule: "every emailed link is built from the tenant's served address".
// A development installation serves a tenant at its name and a port — which is
// how scripts/e2e.sh serves `localhost` on PLATFORMKIT_E2E_PORT, and the academy
// finding of the walkthrough of record — so a link that keeps the name and drops
// the port opens a different server, or none.
//
// The person asks for a link at acme's name on the port this process listens on;
// the link they are mailed has to come back to that same address.
func TestAMailedLinkCarriesTheAddressTheTenantIsServedAt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the link out of it", c.mail)
	}
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatalf("the listener address %q: %v", cfg.Server.Addr, err)
	}
	served := acmeHost + ":" + port

	if code, body := do(t, cfg, nil, http.MethodPost, served, "/api/v1/auth/password/forgot",
		`{"email":"`+adminEmail+`"}`); code != http.StatusOK {
		t.Fatalf("asking for a link at %s = %d %s, want 200", served, code, body)
	}
	var mail string
	eventually(t, "the reset link to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == adminEmail && strings.Contains(sent.Body, "token=") {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if !strings.Contains(mail, "http://"+served+"/app/auth/reset?token=") {
		t.Errorf("the link was asked for at %s and leads elsewhere:\n%s", served, mail)
	}
}
