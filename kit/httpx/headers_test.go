package httpx_test

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

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEveryResponseCarriesTheSecurityHeaders is the whole of the middleware:
// the three unconditional headers on a JSON body, a static file and a route
// that does not exist, and the content security policy on the one kind of
// response that can execute anything.
func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true

	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Public(), ok)

	// An HTML response, written by hand the way modules/admin's shell writes
	// one: a document, so it is the response that gets a policy.
	httpx.Register(api, huma.Operation{
		OperationID: "read-page", Method: http.MethodGet, Path: "/page",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		nonce := httpx.NonceFrom(ctx)
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/html; charset=utf-8")
			hctx.SetStatus(http.StatusOK)
			_, _ = hctx.BodyWriter().Write([]byte(`<script nonce="` + nonce + `">1</script>`))
		}}, nil
	})

	api.Static("/assets", fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(":root{}")}})

	for _, at := range []string{"/widgets", "/page", "/assets/app.css", "/nothing-here"} {
		h := get(t, router, at).Header()
		switch {
		case h.Get("X-Frame-Options") != "DENY":
			t.Errorf("%s may be framed: %q", at, h.Get("X-Frame-Options"))
		case h.Get("Referrer-Policy") != "strict-origin-when-cross-origin":
			t.Errorf("%s leaks its URL: %q", at, h.Get("Referrer-Policy"))
		case h.Get("X-Content-Type-Options") != "nosniff":
			t.Errorf("%s lets a browser guess what it is", at)
		}
	}

	// The policy is on the document and on nothing else: a JSON body executes
	// nothing, and a policy on every response is a header nobody reads.
	page := get(t, router, "/page")
	csp := page.Header().Get("Content-Security-Policy")
	// base-uri and form-action are the two the review found missing, and they
	// are the two that make the rest hold: without the first an injected <base>
	// retargets every relative URL so 'self' stops meaning this origin, and
	// without the second an injected form posts what a person typed somewhere
	// no source list covers.
	for _, want := range []string{"default-src 'self'", "script-src 'self' 'nonce-",
		"frame-ancestors 'none'", "img-src 'self' data:", "base-uri 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the document's policy is %q, which does not say %s", csp, want)
		}
	}
	if json := get(t, router, "/widgets").Header().Get("Content-Security-Policy"); json != "" {
		t.Errorf("a JSON body carries a document policy: %q", json)
	}

	// The nonce the handler was given is the nonce the policy names, and it is
	// a different one on the next request: a nonce a page could hard-code is a
	// nonce an injection can hard-code too.
	nonce := strings.SplitN(strings.SplitN(csp, "'nonce-", 2)[1], "'", 2)[0]
	if !strings.Contains(page.Body.String(), `nonce="`+nonce+`"`) {
		t.Errorf("the policy names %q and the page wrote %q", nonce, page.Body.String())
	}
	if again := get(t, router, "/page").Header().Get("Content-Security-Policy"); again == csp {
		t.Error("two requests were served the same nonce")
	}

	// Strict-Transport-Security, because this fixture's public host is not a
	// local name. A deployment reached at localhost gets none: a browser told
	// to use https for localhost is a laptop that cannot reach its own
	// application until somebody clears a header there is no way to clear.
	if got := page.Header().Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("the response carries %q, want an HSTS policy at a public host", got)
	}

	// And an authenticated document is nobody's to cache. get() presents a
	// session cookie, which is what makes this response somebody's own.
	if got := page.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("an authenticated document says %q about caching, want no-store", got)
	}
}

// TestALocalDeploymentIsNotToldToUseHTTPS. HSTS is not something a laptop can
// take back: a browser that has been told to use https for a name keeps using
// it until the max-age runs out, and there is no header that says "never mind".
// So a deployment reached at a local name is never told.
func TestALocalDeploymentIsNotToldToUseHTTPS(t *testing.T) {
	admin, app := dbtest.Schema(t)
	_ = admin
	f := &fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, app: app, logs: &lines{}}
	api, router := httpx.New(httpx.Options{
		PublicHost: "platformkit.localhost:8080", Tenants: f, Conn: app,
		Authorize: f, Authenticate: f.authenticate, Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Public(), ok)
	req := httptest.NewRequest(http.MethodGet, "http://platformkit.localhost:8080/widgets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("a local deployment is told %q, and cannot be untold it", got)
	}
}

// TestAHandlerKeepsItsOwnPolicy: modules/file serves an uploaded document under
// a policy that allows nothing at all, and the middleware must not widen it.
func TestAHandlerKeepsItsOwnPolicy(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-blob", Method: http.MethodGet, Path: "/blob",
	}, httpx.Public(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/html")
			hctx.SetHeader("Content-Security-Policy", "default-src 'none'; sandbox")
			hctx.SetStatus(http.StatusOK)
		}}, nil
	})
	if got := get(t, router, "/blob").Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("the handler's own policy became %q", got)
	}
}
