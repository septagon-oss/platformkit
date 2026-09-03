package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
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
	for _, want := range []string{"default-src 'self'", "script-src 'self' 'nonce-", "frame-ancestors 'none'", "img-src 'self' data:"} {
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
	nonce := strings.TrimSuffix(strings.SplitN(csp, "'nonce-", 2)[1], "'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
	if !strings.Contains(page.Body.String(), `nonce="`+nonce+`"`) {
		t.Errorf("the policy names %q and the page wrote %q", nonce, page.Body.String())
	}
	if again := get(t, router, "/page").Header().Get("Content-Security-Policy"); again == csp {
		t.Error("two requests were served the same nonce")
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
