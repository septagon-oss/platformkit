package app

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/stream"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// streamRoleReader uses the Authorizer port's documented transaction accessor,
// as modules/auth does when resolving the roles on the context's principal.
type streamRoleReader struct{ fixture }

func (streamRoleReader) Allowed(ctx context.Context, _ tenancy.Tenant, _ tenancy.Grant) (bool, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || !slices.Contains(p.Roles, "reader") {
		return false, nil
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return false, errors.New("no authorizer transaction")
	}
	return true, tx.DB().Exec("SELECT 1").Error
}

type streamIdentityState struct {
	revoked   atomic.Bool
	refreshed atomic.Bool
}

func openStreamRoute(t *testing.T, heartbeat time.Duration) (*db.Conn, *streamIdentityState, <-chan string) {
	t.Helper()
	_, opts := compose(t)
	f := opts.Tenants.(fixture)
	_, conn := dbtest.Schema(t)
	identity := new(streamIdentityState)
	authenticate := func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		p := tenancy.Principal{UserID: f.tenant.ID}
		if !identity.revoked.Load() {
			p.Roles = []string{"reader"}
		} else {
			identity.refreshed.Store(true)
		}
		return p, true, nil
	}
	reader := streamRoleReader{fixture: f}
	api, router := httpx.New(httpx.Options{
		Tenants: f, Conn: conn, Authorize: reader, Authenticate: authenticate,
		Cache: cache.Memory("stream-route"), PublicHost: tenantHost, Log: opts.Log,
	})
	limits := stream.DefaultLimits()
	limits.Heartbeat = heartbeat
	hub := stream.New(stream.Deps{
		Transport: memory.New(),
		Registry: stream.NewRegistry([]module.Module{{Name: "task", Streams: []module.Stream{
			{Event: "task.closed", Policy: module.StreamPermission("task:read")},
			{Event: "task.opened", Policy: module.StreamAnyPrincipal()},
		}}}),
		Authority: streamAuthority{
			conn:         func(context.Context) (*db.Conn, bool) { return conn, true },
			authenticate: authenticate, authorize: reader,
		},
		Limits: limits, Log: opts.Log,
	})
	mountStream(api, func() (*stream.Hub, error) { return hub, nil }, opts.Log)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/app/stream?topics=task.closed,task.opened", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = tenantHost
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", res.StatusCode)
	}
	lines := make(chan string, 128)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream ended before its opening resync")
			}
			if line == "event: resync" {
				return conn, identity, lines
			}
		case <-ctx.Done():
			t.Fatal("stream never sent its opening resync")
		}
	}
}
