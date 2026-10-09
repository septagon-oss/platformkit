package main

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// A set-password link opens a session, so what bounds the link bounds the
// session: one tenant's invitation, spent at another tenant's address, signs
// nobody in anywhere; and one link spent twice at once opens exactly one session.
func TestASetPasswordLinkOpensOneSessionInItsOwnTenant(t *testing.T) {
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
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}

	const email = "spends.a.link@acme.localhost"
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, usersPath,
		`{"email":"`+email+`","displayName":"Spends A Link"}`)
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
	token := tokenIn(t, mail)
	reset := `{"token":"` + token + `","new":"the password this person chose"}`

	client := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}

	// Acme's link at Globex's address: the token row is invisible there.
	stranger := client()
	if code, body = do(t, cfg, stranger, http.MethodPost, globexHost, "/api/v1/auth/password/reset", reset); code == http.StatusOK {
		t.Errorf("acme's link spent at globex = %d %s, want a refusal", code, body)
	}
	for _, host := range []string{globexHost, acmeHost} {
		if code, body = do(t, cfg, stranger, http.MethodGet, host, "/api/v1/auth/me", ""); code != http.StatusForbidden {
			t.Errorf("after spending acme's link at globex, /me at %s = %d %s, want 403", host, code, body)
		}
	}

	// The same link twice at once, at its own address: one session, never two.
	var wg sync.WaitGroup
	codes, bodies := make([]int, 2), make([]string, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], bodies[i] = do(t, cfg, client(), http.MethodPost, acmeHost, "/api/v1/auth/password/reset", reset)
		}()
	}
	wg.Wait()
	opened := 0
	for i := range 2 {
		if codes[i] == http.StatusOK && strings.Contains(bodies[i], `"signedIn":true`) {
			opened++
		}
	}
	if opened != 1 {
		t.Errorf("one link spent twice at once opened %d sessions: %d %s / %d %s",
			opened, codes[0], bodies[0], codes[1], bodies[1])
	}
}
