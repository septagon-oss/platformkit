package main

// Reviewer's case for the fourth review of T-0024 (three surfaces by path). Reviewer: a
// fresh pi session, 2026-09-21.
//
// The third review closed its finding 1 (a person who is signed in and lacks a grant is
// shown the shell's page, not a body of JSON) at a composed kernel, and wrote down what it
// could not prove: "no run here signed in to a running application as an account that lacks
// a grant and watched the answer arrive", because every account this application
// provisions is an administrator. Round 6 composed password signup with the member role
// into the reference application, which supplies exactly that person: nobody invited,
// holding the tenant's ordinary member role — which the seed grants nothing
// (modules/auth/internal/seed.go:27, and e2e/admin-roles.spec.ts:39 says the same in the
// browser) — and so refused every generated screen in the workspace.
//
// The reachability half is therefore now runnable, and it is run at the shipped
// composition: sign up through the door the alias table vouches for, confirm the mailbox
// link the application kept, sign in, and ask a screen the way a browser does. The
// comparison is an administrator asking the same address with the same Accept header, so
// what separates the two answers is the grant alone. The assertions read what the fixed
// behaviour prints — a status, a media type, the shell's own markup and way out — never
// what a broken answer printed.

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// round4Password is a member's own chosen secret: written by the signup form, hashed by the
// user module, read back by nobody here.
const round4Password = "a-member-chooses-this-password"

// navigated is what a browser sends when somebody types an address.
const navigated = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

func TestASignedInPersonWithoutTheGrantIsShownAPageAtTheReferenceApplication(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the confirmation link out of the mailbox it keeps", c.mail)
	}
	email := "round4.member@acme.test"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "Round Four Member",
		"password": round4Password, "confirmation": round4Password, "termsAccepted": true,
	})
	if err != nil {
		t.Fatalf("marshal the signup: %v", err)
	}

	// The door the alias table vouches for, reached at the tenant's own host. An unverified
	// account is the point: nothing but the mailbox link turns it into somebody who can
	// sign in, which is what the composition's own comment claims.
	code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup))
	if code != http.StatusAccepted {
		t.Fatalf("signup at the reference application = %d %s, want the neutral 202", code, body)
	}

	link := ""
	eventually(t, "the confirmation link reaches the mailbox", func() bool {
		for _, sent := range box.Sent() {
			if sent.To != email {
				continue
			}
			if at := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(sent.Body); len(at) == 2 {
				link = at[1]
				return true
			}
		}
		return false
	})
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/verify-email", `{"token":"`+link+`"}`); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("confirming the mailbox link = %d %s, want it to activate the account this composition just accepted", code, body)
	}

	member := signIn(t, cfg, acmeHost, email, round4Password)
	const screen = "/app/task/tasks"

	// Reachability, independent of the answer under test: the administrator, same address,
	// same Accept, is shown the screen.
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, ctype, body := show(t, cfg, admin, acmeHost, screen); code != http.StatusOK {
		t.Fatalf("%s for the administrator = %d %s, want 200; this case is about that address: %s", screen, code, ctype, body)
	}

	code, ctype, body := show(t, cfg, member, acmeHost, screen)
	if code != http.StatusForbidden {
		t.Fatalf("%s for a member holding no grant = %d %s, want the verdict 403: %s", screen, code, ctype, body)
	}
	if strings.HasPrefix(ctype, "application/problem+json") || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("a person who navigated to a screen they may not see was handed %s rather than the shell's page: %s", ctype, body)
	}
	if !strings.Contains(body, httpxCodeDenied) {
		t.Errorf("the page a refused person is shown carries no code to read back: %s", body)
	}
}

// show asks one address the way a navigating browser does and returns the verdict with the
// media type the server declared and the body it sent.
func show(t *testing.T, cfg config.Config, client *http.Client, host, path string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", navigated)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the answer at %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// httpxCodeDenied is spelled here rather than imported: this is the code the authorization
// guard publishes, and the page a refused person is shown has to carry it.
const httpxCodeDenied = "AUTH_DENIED"
