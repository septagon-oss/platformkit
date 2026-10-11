package main

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestEveryAccountDoorPostsToAnAddressThatAnswers is the other half of the shell's
// own promise. Every page beside the sign-in card posts JSON from
// ui/assets/js/session.js, which posts with `redirect: "error"` on purpose: a lost
// response does not establish whether a write committed, and a controller that
// follows a redirect into a second credentialed POST is a person charged twice.
//
// That leaves an address the page may not name. The workspace spelling of a door the
// auth module mounts on its public face — /api/v1/auth/verify-email,
// /api/v1/auth/password/forgot — is a row of kit/httpx/aliases.go, which answers a
// redirect and never a body, because a screen served at two addresses gives one form
// two namespaces. Serving the page is therefore not enough: a form can render, be
// clicked, and reach nothing, and the person is told the outcome is unknown. So this
// reads each page as a browser does, takes the action out of the markup it was sent,
// and asks the running application what that address says.
func TestEveryAccountDoorPostsToAnAddressThatAnswers(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	for _, door := range []struct{ page, form string }{
		{"/app/admin/login", "login"},
		{"/app/admin/register", "register-password"},
		{"/app/admin/login/forgot", "forgot"},
		{"/app/auth/reset", "reset"},
		{"/app/auth/verify-email", "verify-email"},
	} {
		body := served(t, cfg, door.page)
		action := formAction(t, door.page, door.form, body)

		// The body is deliberately one nothing accepts: what is being read is the
		// verdict on the address, and 422 and 401 are verdicts that say somebody is
		// home. A redirect or a 404 says the page named a door nobody serves. The client
		// refuses to follow a redirect because the browser that runs this page's script
		// does: ui/assets/js/session.js posts with redirect: "error".
		doors := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		code, answer := do(t, cfg, doors, http.MethodPost, acmeHost, action, `{"email":"nobody@acme.localhost"}`)
		switch {
		case code >= 300 && code < 400:
			t.Errorf("%s posts to %s, which answers %d: an alias redirects, and ui/assets/js/session.js refuses a redirect, so the person holding that page is told the outcome is unknown. Post to the mounted address: %s",
				door.page, action, code, answer)
		case code == http.StatusNotFound || code == http.StatusMethodNotAllowed:
			t.Errorf("%s posts to %s, which answers %d: the form on the page leads to nothing", door.page, action, code)
		}
	}
}

// served is the page as the browser that will run its script receives it.
func served(t *testing.T, cfg config.Config, page string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+page, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host, req.Header["Accept"] = acmeHost, []string{"text/html"}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", page, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", page, res.StatusCode, body)
	}
	return string(body)
}

var (
	actionPattern = regexp.MustCompile(`action="([^"]+)"`)
	formKind      = regexp.MustCompile(`data-(?:auth|login)-form="([^"]*)"`)
)

// formAction is the address the page's own controller will post to. The login card
// carries the legacy `data-login-form` spelling and no value, so a form with no kind
// is the login form.
func formAction(t *testing.T, page, want, body string) string {
	t.Helper()
	for _, block := range strings.Split(body, "<form") {
		kind := ""
		if m := formKind.FindStringSubmatch(block); m != nil {
			kind = m[1]
		} else if !strings.Contains(block, "data-login-form") {
			continue
		}
		if kind != "" && kind != want {
			continue
		}
		m := actionPattern.FindStringSubmatch(block)
		if m == nil {
			t.Fatalf("%s holds no action on its %s form", page, want)
		}
		return m[1]
	}
	t.Fatalf("%s holds no %s form:\n%s", page, want, body)
	return ""
}
