// Two API values over one store: the brief's Done-when, in the shape the kernel
// actually runs in. cachetest proves the port shares and forgets; this proves the
// wiring — that the host resolution kit/httpx caches is the one the other replica
// reads, and that the invalidation a control-plane route runs reaches a process
// that did not run it.
package httpx_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// node is one replica: the API it serves from and the router in front of it.
type node struct {
	api    *httpx.API
	router *chi.Mux
}

// TestASuspensionReachesTheReplicaThatDidNotTakeIt is the claim the port exists
// for. Two replicas resolve one host and both cache the answer; the operator
// suspends it through the first; the second must stop serving it rather than
// finish the entry's TTL. A local map would answer 200 for half a minute after the
// installation had stopped believing the host existed.
func TestASuspensionReachesTheReplicaThatDidNotTakeIt(t *testing.T) {
	_, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	// One store, two readers: the shape a deployment has when it points two
	// processes at one cache server.
	shared := cache.Memory("pkit")
	f.signedIn()
	f.allow = true

	replica := func(id string) node {
		api, router := httpx.New(httpx.Options{
			Cache:        shared,
			Installation: host,
			PublicHost:   host,
			Tenants:      f,
			Conn:         app,
			Authorize:    f,
			Entitle:      f,
			Authenticate: f.authenticate,
			Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		httpx.Register(api.Surfaces(probe).App, huma.Operation{
			OperationID: id, Method: http.MethodGet, Path: "/ping",
		}, httpx.Permission("widget:read"), ok)
		return node{api: api, router: router}
	}
	a, b := replica("ping_a"), replica("ping_b")

	// Both read the host, and the loader is asked once between them: the second
	// replica read the first one's answer. That is what makes the rest of this case
	// worth anything — there has to be a cached belief to invalidate.
	for _, n := range []node{a, b} {
		if code := get(t, n.router, at(n.api, "/ping")).Code; code != http.StatusOK {
			t.Fatalf("GET /ping = %d, want 200", code)
		}
	}
	if n := f.loads.Load(); n != 1 {
		t.Fatalf("the loader was asked %d times for two replicas, want 1: the cache is not shared", n)
	}

	// The suspension: the host stops resolving, and the operator's own replica
	// invalidates it — in modules/tenant that is the route calling InvalidateHost
	// once the write has committed.
	f.loadErr = tenancy.ErrNoSuchHost
	if err := a.api.InvalidateHost(host); err != nil {
		t.Fatalf("InvalidateHost: %v", err)
	}

	// The replica that never saw the suspension answers 404: what it holds reads
	// as a miss, and the loader, which is the truth, says nobody lives at this host
	// any more.
	res := get(t, b.router, at(b.api, "/ping"))
	if res.Code != http.StatusNotFound {
		t.Errorf("the second replica still serves a suspended host: %d %s; the invalidation reached one process only", res.Code, res.Body)
	}
	if n := f.loads.Load(); n != 2 {
		t.Errorf("the loader was asked %d times in total, want 2: the second replica answered from the belief it holds", n)
	}
}
