package httpx_test

// Reviewer's cases for the second review of T-0024 (three surfaces by path).
// Written to falsify what the change asserts about itself, in kit/httpx/surfaces.go,
// kit/httpx/schemas.go, kit/httpx/httpx.go and kit/httpx/README.md. Each case reaches
// its assertion through what correct behaviour prints — a status, the host's own refusal
// body, the bytes of a file the tree does hold — and never through the output of the
// defect it reports. Reviewer: a fresh pi session, 2026-09-21.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// anonymousAPI is the fixture with nobody to recognise a caller: every request that
// arrives is the anonymous request it is, at either host.
func anonymousAPI(t *testing.T) http.Handler {
	t.Helper()
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
		Installation: installationHost,
		Tenants: loaderFunc(func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
			return who, nil
		}),
		Conn: conn,
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Schema:    entity.Schema{Module: "billing", Entity: "plan", Path: "/api/v1/billing/plans"},
		WritePath: "/api/v1/ops/billing/plans",
	})
	httpx.Register(api.Surfaces("billing").App, huma.Operation{
		OperationID: "r2-read-plans", Method: http.MethodGet, Path: "/plans",
	}, httpx.Permission("billing:read"), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

func askPost(t *testing.T, h http.Handler, authority, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://"+authority+path, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestAnUnrecognisedCallerIsNotToldWhereAnotherSurfaceWrites.
//
// schemas.go: writeElsewhere "answers \"\" in every other case, and one case is refused
// on purpose: the installation's own address is not named to a caller standing at a host
// the control plane is not served at. An address nobody can reach is not a direction
// worth giving". The guard asks the request's *host* and never the *caller*, so at the
// installation host an anonymous POST at the read door of a split resource is answered
// 403 with the control-plane address in the detail — a direction to the operator's door
// handed to somebody who has not been recognised, while the same anonymous caller at any
// other address of that host is refused as anonymous. README.md's App bullet is the
// promise: "App recognises the caller, refuses an anonymous caller except at a route
// that declares Public()".
//
// The passing branch is to make the pointer a direction for a caller who can take it —
// refuse the unrecognised caller the way the surface refuses everybody, or gate the
// pointer on the same recognition that gates the write — and to keep naming it to the
// caller who holds a principal, which TestTheCatalogNamesWhereTheWritesOfAResourceAre
// holds the composition to.
func TestAnUnrecognisedCallerIsNotToldWhereAnotherSurfaceWrites(t *testing.T) {
	h := anonymousAPI(t)

	// Reachability, with none of the defect's output in it: this is the read door
	// (it refuses the verb rather than the address), and an unrecognised caller at a
	// host that serves no control plane is already refused without a pointer. True
	// today and after any fix.
	elsewhere := askPost(t, h, host, "/api/v1/billing/plans")
	if elsewhere.Code != http.StatusMethodNotAllowed {
		t.Fatalf("an anonymous write at the read door of a host with no control plane = %d %s; "+
			"this case is about that door", elsewhere.Code, elsewhere.Body.String())
	}
	if strings.Contains(elsewhere.Body.String(), "/api/v1/ops/") {
		t.Fatalf("a host that serves no control plane named one: %s", elsewhere.Body.String())
	}

	got := askPost(t, h, installationHost, "/api/v1/billing/plans")
	if body := got.Body.String(); strings.Contains(body, "/api/v1/ops/") {
		t.Errorf("an anonymous POST at the read door of the installation host was handed %q; the pointer "+
			"exists for the caller who derived the read door from the catalog and can use the write door, "+
			"and this caller holds no principal — the surface refuses an anonymous caller everywhere else "+
			"on this host", strings.TrimSpace(body))
	}
}

// TestAMissingFileUnderAMountedTreeTakesTheRefusalOfAnAddressNobodyMounted.
//
// httpx.go's own account of root.NotFound: these are "the refusals a browser runs into
// most often — a mistyped address, a bookmark left over from an earlier deployment" and
// "none of them reaches a handler, so the answer used to be net/http's plain-text '404
// page not found' … Through a.fail they are the same verdict as every other refusal and
// take the shape the client asked for, which is also what keeps a monitor seeing a
// problem document it can parse". A file tree is now mounted on that same root router,
// inside the surface's classification and headers (README.md), and http.FileServerFS
// answers a missing file for itself: the person gets net/http's note, the monitor gets no
// problem document, and the registered Fault is never consulted.
//
// The passing branch is to answer the tree's missing file through a.fail like every
// other refusal of the host — the same status, the same body, the same shape.
func TestAMissingFileUnderAMountedTreeTakesTheRefusalOfAnAddressNobodyMounted(t *testing.T) {
	s := newSurfaces(t)
	tree := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(":root{--a:1}")}}
	s.api.Surfaces("admin").App.Static("/assets", tree)
	s.api.Surfaces("web").Public.Static("/sheets", tree)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the trees do not describe themselves: %v", err)
	}

	// Reachability first, and it is the tree working: the file it holds is served, at
	// both surfaces, whatever this case decides about the file it does not hold.
	for _, at := range []string{"/app/admin/assets/app.css", "/web/sheets/app.css"} {
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+at, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "--a") {
			t.Fatalf("GET %s = %d %q; this case is about a tree that serves its own files", at, w.Code, w.Body.String())
		}
	}

	ask := func(path, accept string) (int, string, string) {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, r)
		return w.Code, w.Header().Get("Content-Type"), strings.TrimSpace(w.Body.String())
	}
	for _, tc := range []struct{ surface, missing, unmounted string }{
		{"workspace", "/app/admin/assets/absent.css", "/app/admin/nothing-mounted-here"},
		{"public face", "/web/sheets/absent.css", "/web/nothing-mounted-here"},
	} {
		wantCode, wantType, _ := ask(tc.unmounted, "application/json")
		code, ctype, body := ask(tc.missing, "application/json")
		if code != wantCode || ctype != wantType || !strings.Contains(body, "nothing is served at this address") {
			t.Errorf("a file missing from the %s tree answers %d %q %q where an address nobody mounted on the "+
				"same host answers %d %q: the tree's own 404 is outside the one shape this package promises a "+
				"client it can parse", tc.surface, code, ctype, body, wantCode, wantType)
		}
		if _, ctype, body = ask(tc.missing, "text/html"); strings.Contains(body, "404 page not found") || strings.Contains(ctype, "text/plain") {
			t.Errorf("a browser navigating to the missing %s file is shown %q (%s): the plain-text note this "+
				"package says no browser is shown any more", tc.surface, body, ctype)
		}
	}
}

// TestThePublicWriteLimitCountsTwoTenantsApartInTheCounterItWritesTo is the pin over the
// promise the first review called High: README.md's "counted by tenant, route and
// address", and middleware.go's "two customers therefore never share a counter". The
// existing cases read the *key* the kernel composes, with a double in place of the
// counter; this one uses limit.Postgres — the limiter kit/app actually wires — so the
// statement that decides the answer, and the tenant kit/limit scopes a stored key with,
// are both under test. Two tenants on two hosts, one visitor address: one counter each.
func TestThePublicWriteLimitCountsTwoTenantsApartInTheCounterItWritesTo(t *testing.T) {
	const otherHost = "globex.test"
	admin, conn := dbtest.Schema(t)
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
		Installation: installationHost,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h == otherHost {
				return globex, nil
			}
			return acme, nil
		}),
		Conn: conn,
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
		WriteLimiter: limit.Postgres(func(context.Context) (*db.Conn, bool) {
			return conn, true
		}),
	})
	httpx.Register(api.Surfaces("auth").Public, huma.Operation{
		OperationID: "r2-register", Method: http.MethodPost, Path: "/register",
	}, httpx.Public(), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	write := func(authority string) int {
		r := httptest.NewRequest(http.MethodPost, "http://"+authority+"/api/v1/public/auth/register", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}
	refused := 0
	for i := 1; i <= 61; i++ {
		if write(host) == http.StatusTooManyRequests {
			refused = i
			break
		}
	}
	if refused == 0 {
		t.Fatal("61 anonymous writes from one address were never refused; this case asks who the limit counted")
	}
	if got := write(otherHost); got == http.StatusTooManyRequests {
		t.Errorf("the second tenant's first anonymous write from the same visitor address = %d; one visitor "+
			"is not one customer, and README.md says the counter is held by tenant", got)
	}

	// And the rows the counter actually wrote: two, each owned by the tenant the
	// request resolved to (kit/limit scopes the stored key with the tenant id, which
	// is the half a double in place of the counter cannot show).
	var rows, owned int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*),
		count(*) FILTER (WHERE split_part(key, '/', 1) IN ($1, $2))
		FROM platformkit_limits`, acme.ID.String(), globex.ID.String()).Scan(&rows, &owned); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || owned != 2 {
		t.Errorf("platformkit_limits holds %d rows of which %d name one of the two tenants; the promise is one "+
			"row per tenant for one visitor address, and a row keyed by nothing but the address is what the "+
			"first review found", rows, owned)
	}
}
