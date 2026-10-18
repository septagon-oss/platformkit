package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestAPersonCreatedFromTheShellIsSentTheInvitation is the brief's first decided
// rule: "creating a person in the admin shell sends the existing invitation by
// default", and "the invited person sets a password from the mail". The shell's
// users screen creates through the users collection — the generated New form posts
// to it — so that is the door this case knocks on, as the tenant's administrator,
// with nothing but an address and a name.
//
// It passes when the person the shell made is mailed a link on their tenant's own
// host, and the token in it sets the password they then sign in with — with no
// administrator choosing a password for them.
func TestAPersonCreatedFromTheShellIsSentTheInvitation(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the invitation out of it", c.mail)
	}
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	const email = "miguel.shell@acme.localhost"
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, usersPath,
		`{"email":"`+email+`","displayName":"Miguel"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s as the administrator = %d %s, want 201", usersPath, code, body)
	}

	var mail string
	eventually(t, "the invitation of the person the shell created to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == email {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if !strings.Contains(mail, "http://"+acmeHost+"/app/auth/reset?token=") {
		t.Errorf("the invitation does not lead to acme's set-password page:\n%s", mail)
	}

	const chosen = "a password Miguel chose himself"
	redeemMailedLink(t, cfg, nil, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, mail)+`","new":"`+chosen+`"}`)
	signIn(t, cfg, acmeHost, email, chosen)
}
