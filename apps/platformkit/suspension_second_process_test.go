package main

import (
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestASuspensionThroughTheRouteStopsTheOtherProcessServingTheHost is the brief's
// "a suspension invalidates on a second process", run the way an operator runs it:
// two applications booted from one configuration over one database and one shared
// store, a host resolved by both, and the suspension sent to the first through
// POST /api/v1/ops/tenant/tenants/{id}/suspend. The second process never saw the
// request; it must stop serving the host now, not when its belief expires.
//
// Both answers are status codes the fixed behaviour prints: the tenant's host is a
// 403 to an anonymous list while the tenant is served, and the 404 of an address
// nobody serves once it is suspended.
func TestASuspensionThroughTheRouteStopsTheOtherProcessServingTheHost(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_VALKEY_URL")
	if url == "" {
		t.Skip("PLATFORMKIT_TEST_VALKEY_URL is unset; `make up` starts the valkey service this case reads")
	}
	path, cfg := configure(t)
	install(t, path)
	// One application name for both processes, and one no other run shares, so
	// what this case reads is what these two processes wrote.
	cfg.Cache = config.Cache{Adapter: "valkey", App: "susp-" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36), URL: url}

	first := compose(cfg)
	start(t, cfg, first.modules, app.Options{
		Tenants: first.tenants, Authorize: first.auth, Entitle: first.plans, Authenticate: first.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(), Caches: caches(),
	})
	other := cfg
	other.Server.Addr = freeAddr(t)
	second := compose(other)
	start(t, other, second.modules, app.Options{
		Tenants: second.tenants, Authorize: second.auth, Entitle: second.plans, Authenticate: second.auth.Authenticate,
		Role: app.Web, Transport: memory.New(), Log: quiet(), Caches: caches(),
	})

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := field(t, body, "id")

	// Both processes resolve the host and believe it.
	for _, c := range []config.Config{cfg, other} {
		if code, body = do(t, c, nil, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusForbidden {
			t.Fatalf("an anonymous list at globex on %s = %d %s, want 403: the host is served", c.Server.Addr, code, body)
		}
	}

	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+globexID+"/suspend", ""); code != http.StatusOK {
		t.Fatalf("suspend = %d %s, want 200", code, body)
	}

	// Well inside hostTTL: a belief only the first process forgot is still a 403 here.
	if code, body = do(t, other, nil, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("the second process at a suspended host = %d %s, want 404: the suspension did not reach it", code, body)
	}
	if code, body = do(t, cfg, nil, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("the first process at a suspended host = %d %s, want 404", code, body)
	}
}
