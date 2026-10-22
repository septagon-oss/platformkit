package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestASessionThatRanOutIsGreetedWithSignInAgainOnTheWayBack: a person whose session ran
// out of time while they were away is sent to sign-in from the page they opened, told to
// sign in again, and the form carries them back. What the browser still holds at that
// moment is decided by the cookies sign-in set: a cookie whose Max-Age ends no later than
// the session it names is gone by the time the server would refuse that session.
func TestASessionThatRanOutIsGreetedWithSignInAgainOnTheWayBack(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	issued, signedAt := signInCookies(t, cfg)

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var expires time.Time
	if err := owner.QueryRowContext(t.Context(), `SELECT max(expires_at) FROM sessions`).Scan(&expires); err != nil {
		t.Fatalf("read the session's expiry: %v", err)
	}
	// The browser at the moment the session expires: a cookie with no Max-Age lives as long
	// as the browser does, and one with a Max-Age lives until sign-in plus that many seconds.
	var held []*http.Cookie
	for _, k := range issued {
		t.Logf("sign-in set %s with Max-Age %ds; the session it names expires %s after sign-in",
			k.Name, k.MaxAge, expires.Sub(signedAt).Round(time.Second))
		if k.MaxAge == 0 || signedAt.Add(time.Duration(k.MaxAge)*time.Second).After(expires) {
			held = append(held, k)
		}
	}
	if _, err := owner.ExecContext(t.Context(), `UPDATE sessions SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("let the session run out: %v", err)
	}

	page := "/app/task/tasks"
	body := pushedOff(t, cfg, page, held)
	if !strings.Contains(body, signInAgain) || strings.Contains(body, signInStranger) {
		t.Errorf("a person whose session ran out is greeted as a stranger; cookies held at expiry: %d of %d issued: %s",
			len(held), len(issued), trimHTML(body))
	}
}
