package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// storeThatRefusesTheFirstClose reads and writes and refuses exactly one Move,
// which is the outage the route below has to answer and the recovery it has to
// leave open. The bool is the store's own state, not the test's: one instance is
// built at the boot and every request reaches that one.
type storeThatRefusesTheFirstClose struct {
	cache.Cache
	refused atomic.Bool
}

func (*storeThatRefusesTheFirstClose) Close() error { return nil }

func (s *storeThatRefusesTheFirstClose) Move(ctx context.Context, scope cache.Scope) error {
	if s.refused.Swap(false) {
		return errors.New("the shared store did not accept the invalidation")
	}
	return s.Cache.Move(ctx, scope)
}

// TestALocaleSetCannotReportCompleteSuccessWhenInvalidationFails is the second
// route that closes the host namespace, run against a store that refuses the
// close once. It pins the three things that answer such a failure.
//
// The answer is not 200. The languages travel inside the cached host resolution,
// so the next page at that host is served in the languages the installation had
// before this request, at this process and at every other one reading the same
// store — the same window
// TestASuspensionCannotReportCompleteSuccessWhenInvalidationFails catches at the
// suspend route, at the only other call site. Nor is the answer a refusal that
// unwinds the write: the operator said what this tenant speaks, the row says it,
// and a refusal that took it back would leave a company that cannot say what it
// speaks because a cache stopped answering. And the 503 has to be worth sending:
// repeating the request once the store takes the close again finishes the change,
// because the write is idempotent and the move runs whether or not the row moved.
func TestALocaleSetCannotReportCompleteSuccessWhenInvalidationFails(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "locale-failed-move", URL: "valkey://127.0.0.1:6379"}
	backend := cache.MemoryBackend()
	t.Cleanup(func() { _ = backend.Close() })
	store := &storeThatRefusesTheFirstClose{Cache: cache.MemoryNamed(cfg.Cache.App, backend)}
	store.refused.Store(true)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
		Caches: app.Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) { return store, nil }},
	})

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create the tenant = %d %s, want 201", code, body)
	}
	id := field(t, body, "id")
	// Warm the resolution the way a served host warms it, so the close the route
	// owes is one that is actually sitting in the store.
	if code, body = do(t, cfg, nil, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusForbidden {
		t.Fatalf("warm the served host = %d %s, want 403", code, body)
	}

	locale := `{"default":"pt-PT","supported":["en","pt-PT"]}`
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id+"/locale", locale); code != http.StatusServiceUnavailable {
		t.Errorf("set the locale over a store that refuses the close = %d %s, want 503: the next page is still served in the older languages", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tenantPath+"/"+id, ""); code != http.StatusOK ||
		!strings.Contains(body, `"defaultLocale":"pt-PT"`) {
		t.Errorf("the refusal took the record back with it: %d %s", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id+"/locale", locale); code != http.StatusOK {
		t.Errorf("the same request once the store answers again = %d %s, want 200: the retry does not finish what the refusal named", code, body)
	}
}
