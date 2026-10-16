package main

import (
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// After accepting the mailed invitation, the person has a session of their
// own and can enter the workspace without a second credential form.
func TestAcceptingAnInvitationSignsThePersonIn(t *testing.T) {
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
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const email = "accepts.invitation@acme.localhost"
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, usersPath,
		`{"email":"`+email+`","displayName":"Accepts Invitation"}`)
	if code != http.StatusCreated {
		t.Fatalf("create a person = %d %s, want 201", code, body)
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
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	person := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	const chosen = "the password this person chose"
	redeemMailedLink(t, cfg, person, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, mail)+`","new":"`+chosen+`"}`)
	code, body = do(t, cfg, person, http.MethodGet, acmeHost, "/api/v1/auth/me", "")
	if code != http.StatusOK {
		t.Errorf("accepting the invitation left the person signed out: /me = %d %s", code, body)
	}
}
