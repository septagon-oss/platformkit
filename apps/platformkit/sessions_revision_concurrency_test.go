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

// This decorator pauses after the real PostgreSQL read. Every list and mutation
// still goes through the composed auth service; it adds no alternative store.
type pausedSessionList struct {
	authcontracts.Auth
	armed   atomic.Bool
	read    chan struct{}
	release chan struct{}
}

func (s *pausedSessionList) Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*authcontracts.SessionListing, error) {
	list, err := s.Auth.Sessions(ctx, tx, userID, current)
	if err == nil && s.armed.CompareAndSwap(true, false) {
		close(s.read)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return list, err
}

func TestASessionCreatedAfterTheRevisionReadMakesTheRevocationRefuse(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	sessions := &pausedSessionList{Auth: c.auth, read: make(chan struct{}), release: make(chan struct{})}
	finish := sync.OnceFunc(func() { close(sessions.release) })
	defer finish()
	// Replace only the shell's session dependency with the scheduling decorator.
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
			if attr.Key == "name" {
				name = attr.Val
			}
			if attr.Key == "value" {
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
	case <-sessions.read:
	case <-time.After(30 * time.Second):
		t.Fatal("the revocation did not reach its authoritative session-list read")
	}
	// This sign-in commits after the command's premise was checked and before its delete.
	later := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	finish()
	out := <-done
	if out.err != nil {
		t.Fatal(out.err)
	}
	defer out.res.Body.Close()
	response, _ := io.ReadAll(out.res.Body)
	if out.res.StatusCode != http.StatusConflict {
		t.Errorf("session list changed inside the command: response = %d %s; want 409", out.res.StatusCode, response)
	}
	for label, device := range map[string]*http.Client{"counted": there, "later": later} {
		if code, body := do(t, cfg, device, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code != http.StatusOK {
			t.Errorf("refused revocation ended the %s session: /auth/me = %d %s; want 200", label, code, body)
		}
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var emitted int
	if err := owner.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1", authcontracts.EventSessionRevoked).Scan(&emitted); err != nil {
		t.Fatal(err)
	}
	if emitted != 0 {
		t.Errorf("refused revocation emitted %d events; want 0", emitted)
	}
}
