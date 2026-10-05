package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/admin"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// pausedBeforeRevoke holds the command at its first write, whichever revocation
// it uses, after every reading of the list it makes. Reads and writes still go
// through the composed auth service.
type pausedBeforeRevoke struct {
	authcontracts.Auth
	armed   atomic.Bool
	writing chan struct{}
	release chan struct{}
}

func (s *pausedBeforeRevoke) pause(ctx context.Context) error {
	if !s.armed.CompareAndSwap(true, false) {
		return nil
	}
	close(s.writing)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *pausedBeforeRevoke) RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error {
	if err := s.pause(ctx); err != nil {
		return err
	}
	return s.Auth.RevokeSession(ctx, tx, userID, ref)
}

func (s *pausedBeforeRevoke) RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error {
	if err := s.pause(ctx); err != nil {
		return err
	}
	return s.Auth.RevokeSessions(ctx, tx, userID, except)
}

// The "everywhere else" button ends the sessions its screen counted. A machine that
// signs in after the command's last reading, while the delete is about to run, was
// never on that screen: it stays signed in, and only the counted one leaves the trail.
func TestASessionSignedInBeforeTheDeleteIsNotEndedByTheCountedRevocation(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	sessions := &pausedBeforeRevoke{Auth: c.auth, writing: make(chan struct{}), release: make(chan struct{})}
	finish := sync.OnceFunc(func() { close(sessions.release) })
	defer finish()
	mods := c.modules[:len(c.modules)-1]
	c.modules[len(c.modules)-1] = admin.Module(admin.Deps{
		Modules: mods, Authorize: c.auth, Tenants: c.tenants, Roles: c.auth, Sessions: sessions,
		Theme: design.Default(), Messages: c.messages, SignIn: pinnedSignInAPI,
	})
	opts := appOptions(cfg, c, app.All)
	opts.Transport, opts.Log = memory.New(), quiet()
	start(t, cfg, c.modules, opts)
	here := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	there := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, here, http.MethodGet, acmeHost, "/app/auth/sessions", "")
	if code != http.StatusOK {
		t.Fatalf("sessions page = %d %s", code, body)
	}
	var expected string
	tokens := html.NewTokenizer(strings.NewReader(body))
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		if token.Data != "input" {
			continue
		}
		var name, value string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "name":
				name = attr.Val
			case "value":
				value = attr.Val
			}
		}
		if name == "expected" {
			expected = value
		}
	}
	if expected == "" {
		t.Fatal("the form must carry the revision of the sessions it counted")
	}
	asking := &http.Client{Jar: here.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/app/auth/sessions/revoke-rest", strings.NewReader("expected="+expected))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sessions.armed.Store(true)
	type answer struct {
		res *http.Response
		err error
	}
	done := make(chan answer, 1)
	go func() { res, err := asking.Do(req); done <- answer{res, err} }()
	select {
	case <-sessions.writing:
	case <-time.After(30 * time.Second):
		t.Fatal("the revocation did not reach its first write")
	}
	later := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	finish()
	out := <-done
	if out.err != nil {
		t.Fatal(out.err)
	}
	defer out.res.Body.Close()
	response, _ := io.ReadAll(out.res.Body)
	if out.res.StatusCode != http.StatusSeeOther {
		t.Errorf("revocation of the counted list = %d %s; want 303", out.res.StatusCode, response)
	}
	for label, device := range map[string]*http.Client{"answering": here, "later": later} {
		if code, body := do(t, cfg, device, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code != http.StatusOK {
			t.Errorf("the %s session was ended: /auth/me = %d %s; want 200", label, code, body)
		}
	}
	if code, _ := do(t, cfg, there, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code == http.StatusOK {
		t.Error("the counted session is still signed in; the button ends it")
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var emitted int
	if err := owner.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1", authcontracts.EventSessionRevoked).Scan(&emitted); err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Errorf("the counted revocation emitted %d events; want 1, for the one session it counted", emitted)
	}
}
