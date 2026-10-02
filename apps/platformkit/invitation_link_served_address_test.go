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

// An invitation made through the users screen must return to the address
// where the administrator made it, including a development listener's port.
func TestAnInvitationLinkCarriesTheAddressTheTenantIsServedAt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T", c.mail)
	}
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	served := acmeHost + ":" + port
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	const email = "invited.at.served.address@acme.localhost"
	code, body := do(t, cfg, admin, http.MethodPost, served, usersPath,
		`{"email":"`+email+`","displayName":"Served Address"}`)
	if code != http.StatusCreated {
		t.Fatalf("create a person at %s = %d %s, want 201", served, code, body)
	}

	var mail string
	eventually(t, "the invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == email {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if want := "http://" + served + "/app/auth/reset?token="; !strings.Contains(mail, want) {
		t.Errorf("the invitation asked for at %s leads elsewhere:\n%s", served, mail)
	}
}
