package httpx_test

// Review round 13 (T-0110). What this round is the first to be able to ask.
//
// `7fc5e34` merged `origin/main` (T-0111, a locale per tenant) over this branch's
// refusal counting, and the merge landed inside the two functions that decide who
// counts a refusal: `fail` counts only when the response is not held in a buffer
// (`if _, held := bufferFrom(r.Context()); !held { countRefusal(...) }`), `refuse`
// counts nothing and lets `respond` count the settled status once, and — new with
// the merge — `show` resolves the refusal's tenant (`withHostTenant`) before it
// hands the verdict to the registered renderer, because the page it renders has a
// language the tenant declares.
//
// Rounds 1, 2, 8 and 11 pinned the counter for refusals answered as JSON: the
// router's own 400/404/405, a guard's 403, a handler's error and panic, an
// unresolved host, a forged baggage. Every one of those fixtures registers no
// `httpx.Options.Fault`, so `wantsDocument(r)` is answered by `writeProblem` and
// the branch that renders a *document* — the branch the merge edited — is in none
// of them. This file puts a renderer on the same router and asks the two questions
// nobody has asked there:
//
//   - does one refused request that a person was *shown* still move its class by
//     exactly one? `fail` counts before it asks for the page; a renderer whose bytes
//     also settle a status through a writer that counts would move it twice, and a
//     class counted twice is not a number of refused requests;
//   - did that one refusal reach the client as one document? A page written past a
//     buffer that then also sends what it was holding is two bodies in one response
//     — bytes no parser accepts and no JSON-only case would notice.
//
// Each case reaches its assertion through what the *correct* answer prints: the
// status the client was answered, the renderer's own marker in the body, and the
// class the delivery's own table gives that status. No case asks the counter
// whether it moved before asking the router whether it refused, and no case reads
// the defect's output to decide it was reachable.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// pageMarker is what the renderer below writes and nothing else in this package
// can write. It is the reachability probe: a case that does not find it in the body
// never reached the branch it means to test, and says so.
const pageMarker = "<!--round13-->"

// pageFault is a renderer that answers: the status the verdict carries, the marker,
// and nothing but the page. Returning false — the opt-out — is the other branch and
// is round 1's and round 2's fixture, which is what those files already cover.
func pageFault(w http.ResponseWriter, _ *http.Request, p *problem.Problem) bool {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(p.Status)
	_, _ = w.Write([]byte("<!doctype html><html><body>" + pageMarker + "</body></html>"))
	return true
}

// pageFixture is round 2's fixture with one addition: a renderer registered, so a
// browser navigation gets a document. Built here, byte for byte otherwise, so the
// earlier round's own file stays the bytes its author wrote (decision 0008).
func pageFixture(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       app,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenancy.Tenant{ID: reviewTenant, Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return false, nil // every grant is refused: this is the guard's branch
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			// A signed-in caller, so the refusal below is the guard's denial of the
			// grant and not the anonymous branch (which answers 303 at the sign-in page).
			return tenancy.Principal{UserID: reviewTenant, Roles: []string{"reviewer"}}, true, nil
		},
		Fault: pageFault,
		Log:   slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r13_public_query", Method: http.MethodGet, Path: "/public-query",
	}, httpx.Public(), func(context.Context, *reviewInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r13_permitted", Method: http.MethodGet, Path: "/permitted",
	}, httpx.Permission("widget:read"), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r13_panicking", Method: http.MethodGet, Path: "/panicking",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		panic("the handler fell over")
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// TestOneRefusalShownToAPersonMovesItsClassByExactlyOne is the question above, run
// against the branch the merge edited.
func TestOneRefusalShownToAPersonMovesItsClassByExactlyOne(t *testing.T) {
	router := pageFixture(t)
	const base = "http://" + host + "/api/v1/probe"

	cases := []struct {
		what string
		req  func() *http.Request
	}{
		{"nobody mounted that address, and a browser asked to be shown the answer", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, base+"/nobody-mounted-this", nil)
			r.Header.Set("Accept", "text/html")
			return r
		}},
		{"the address is mounted but not for that verb, and a browser asked", func() *http.Request {
			r := httptest.NewRequest(http.MethodDelete, base+"/public-query", nil)
			r.Header.Set("Accept", "text/html")
			return r
		}},
		{"a guard refused the grant the route declares, to a browser", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, base+"/permitted", nil)
			r.Header.Set("Accept", "text/html")
			return r
		}},
		{"a handler panicked in front of a browser", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, base+"/panicking", nil)
			r.Header.Set("Accept", "text/html")
			return r
		}},
	}

	for _, tc := range cases {
		before := review2ClassTotals(t)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, tc.req())

		// The reachability probe, off the answer alone: what the client was shown.
		if rec.Code < http.StatusBadRequest {
			t.Errorf("%s: the client was answered %d, which is not a refusal, so this case "+
				"asked the counter nothing: %s", tc.what, rec.Code, rec.Body.String())
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, pageMarker) {
			t.Errorf("%s: the client was answered %d with a body that is not the registered "+
				"renderer's page, so this case never reached the branch it measures (body %q)",
				tc.what, rec.Code, body)
			continue
		}
		if strings.Contains(body, `"status":`) || strings.Contains(body, `"detail":`) {
			t.Errorf("%s: the client was answered %d with a body that holds the renderer's page "+
				"*and* a problem document — one refusal answered twice into one response, bytes "+
				"no parser accepts (body %q)", tc.what, rec.Code, body)
			continue
		}

		class := telemetry.RefusalClass(rec.Code)
		if class == "unknown" {
			t.Errorf("%s: the client was answered %d, which the closed class set maps to %q",
				tc.what, rec.Code, class)
			continue
		}
		after := review2ClassTotals(t)
		if got := after[class] - before[class]; got != 1 {
			t.Errorf("%s: the client was answered %d (class %q) with a page, and "+
				"pkit.http.refusals moved by %d for that one request, want exactly 1 — fail counts "+
				"the un-held refusal before it asks for the page, respond counts the held one from "+
				"the settled status, and a page that settles one as well counts the same person "+
				"twice", tc.what, rec.Code, class, got)
		}
		delete(after, class)
		for other, delta := range after {
			if delta != before[other] {
				t.Errorf("%s: the client was answered %d (class %q) and class %q moved by %d as "+
					"well — one refusal recorded under two classes is counted twice",
					tc.what, rec.Code, class, other, delta-before[other])
			}
		}
	}
}
