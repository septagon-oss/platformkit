package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

const (
	signInAgain    = "Sign in again to continue."
	signInStranger = "Use the address this tenant knows you by."
)

// browse asks for a page the way a browser does, holding exactly the cookies given and
// following no redirect, and answers the status, the Location and the body.
func browse(t *testing.T, cfg config.Config, path string, cookies []*http.Cookie) (int, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get("Location"), string(body)
}

// pushedOff opens a workspace page with the cookies a browser still holds, follows the
// kernel's 303 to sign-in, and answers the sign-in page. Reaching it is the same before
// and after any wording: the page is a 303 to the sign-in form that carries next.
func pushedOff(t *testing.T, cfg config.Config, page string, cookies []*http.Cookie) string {
	t.Helper()
	code, to, body := browse(t, cfg, page, cookies)
	if code != http.StatusSeeOther || !strings.Contains(to, "next="+url.QueryEscape(page)) {
		t.Fatalf("GET %s with no live session = %d to %q, want a 303 to sign-in that keeps the page: %s", page, code, to, trimHTML(body))
	}
	code, _, body = browse(t, cfg, to, cookies)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", to, code, trimHTML(body))
	}
	return body
}

// signInCookies signs in as acme's administrator and answers the cookies the answer set,
// with the moment they were set.
func signInCookies(t *testing.T, cfg config.Config) ([]*http.Cookie, time.Time) {
	t.Helper()
	signedAt := time.Now()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+cfg.Server.Addr+"/api/v1/auth/login",
		strings.NewReader(`{"email":"`+adminEmail+`","password":"`+adminPass+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || len(res.Cookies()) == 0 {
		t.Fatalf("sign-in = %d with %d cookies", res.StatusCode, len(res.Cookies()))
	}
	return res.Cookies(), signedAt
}

// TestASessionEndedElsewhereIsGreetedWithSignInAgainAndAStrangerIsNot: a cookie that names a
// session the server ended — signed out elsewhere, revoked — gets "Sign in again"; a request
// with no cookie gets the line a first visit gets; a signed-in person is not told to sign in
// again.
func TestASessionEndedElsewhereIsGreetedWithSignInAgainAndAStrangerIsNot(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	held, _ := signInCookies(t, cfg)
	if body := pushedOff(t, cfg, "/app/task/tasks", nil); !strings.Contains(body, signInStranger) || strings.Contains(body, signInAgain) {
		t.Errorf("a first visit is told to sign in again: %s", trimHTML(body))
	}
	if code, _, body := browse(t, cfg, "/app/admin/login", held); code == http.StatusOK && strings.Contains(body, signInAgain) {
		t.Errorf("a signed-in person opening sign-in is told to sign in again: %s", trimHTML(body))
	}

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	if _, err := owner.ExecContext(t.Context(), `DELETE FROM sessions`); err != nil {
		t.Fatalf("end the session: %v", err)
	}
	body := pushedOff(t, cfg, "/app/task/tasks", held)
	if !strings.Contains(body, signInAgain) || strings.Contains(body, signInStranger) {
		t.Errorf("a person whose session was ended is not told to sign in again: %s", trimHTML(body))
	}
	if !strings.Contains(body, `data-next="/app/task/tasks"`) {
		t.Errorf("the sign-in form does not carry the person back: %s", trimHTML(body))
	}
}
