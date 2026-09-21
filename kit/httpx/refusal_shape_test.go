package httpx_test

// The guards inside the huma chain answer with API.refuse, and this is the file that
// holds what that means at the level of this package — the level the reviewer's case
// in ui/page cannot reach, because that one composes a shell and asks whether the
// person was shown a translated page.
//
// What is being held is one rule with two halves. A refusal that runs ahead of a
// handler — the authorization denial, the public write limit, a host that resolves to
// no site, a transaction that did not commit — is the same verdict in either shape:
// a page for a client that came to be *shown* the answer, the problem document for a
// client that asked for a value. The half that is easy to get wrong is the one an
// unwritten case hides: those guards answer inside a transaction that decides whether
// to commit from the status of the response, so a refusal written straight to the
// writer without saying its verdict to the context is rolled forward as an undecided
// response and replaced by a 500. The person asking for a page then gets an outage,
// which is the shape of the second review's finding 1 and the trap its own trial fix
// fell into. Every case below therefore asserts the status as loudly as the shape.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// guardSetup is one composed API with a guarded door, an anonymous public write and a
// renderer registered — the three refusals this file asks about, from one fixture, so
// that the shape each takes is a fact about the client and not about the route.
type guardSetup struct {
	who     tenancy.Tenant
	allow   bool
	limiter httpx.WriteLimiter
	// unresolvable stands for a host lookup that could not answer: an outage, and
	// not a site that does not exist.
	unresolvable bool
}

func guardKernel(t *testing.T, f guardSetup, fault httpx.Fault) (*httpx.API, *chi.Mux) {
	t.Helper()
	_, app := dbtest.Schema(t)
	f.who = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Installation: installationHost, Conn: app,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if f.unresolvable {
				return tenancy.Tenant{}, errors.New("the tenant store is not answering")
			}
			if h == host || h == installationHost {
				return f.who, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return f.allow, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		WriteLimiter: f.limiter,
		Log:          slog.New(slog.DiscardHandler),
		Fault:        fault,
	})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "shape-plans", Method: http.MethodGet, Path: "/plans",
	}, httpx.Permission("billing:read"), ok)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "shape-plan-write", Method: http.MethodPost, Path: "/plans",
	}, httpx.Permission("billing:write"), ok)
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "shape-ask", Method: http.MethodPost, Path: "/ask",
	}, httpx.Public(), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return api, router
}

// askFor sends the same request twice, once as a browser and once as a client, which is
// the only way to say "the shape is the caller's choice" in one place.
func askFor(t *testing.T, router http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	r := httptest.NewRequest(method, "http://"+host+path, body)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Accept", accept)
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

// TestAGrantDenialTakesTheShapeTheCallerAskedFor. The person who navigated to a page
// they hold no grant for is the reader ui/page's faultKeys table exists for, and the
// six session codes in it travel on this one refusal. Before this file's rule they
// arrived as huma's JSON, because the guard ran inside the chain and answered there.
func TestAGrantDenialTakesTheShapeTheCallerAskedFor(t *testing.T) {
	f := guardSetup{allow: true}
	api, router := guardKernel(t, f, documentFault)
	guarded := at(api, "/plans")

	// Reachability, and it is the grant and nothing else that differs between this
	// request and every refusal below: the address is served and this caller holds it.
	if got := askFor(t, router, http.MethodGet, guarded, "application/json"); got.Code != http.StatusOK {
		t.Fatalf("the guarded door for a caller who holds the grant = %d; this case is about that door: %s",
			got.Code, got.Body.String())
	}

	denied := guardSetup{allow: false}
	_, refused := guardKernel(t, denied, documentFault)

	page := askFor(t, refused, http.MethodGet, guarded, browserAccept)
	if page.Code != http.StatusForbidden {
		t.Fatalf("a navigating caller without the grant = %d, want the verdict 403 and not a 500 standing in for "+
			"an undecided response: %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a navigating caller without the grant was handed %s: %s", ct, page.Body.String())
	}
	for _, want := range []string{httpx.CodeDenied + ":", "urn:request:"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the denial page omits %q, which is the half of the line support and the log match on: %s",
				want, page.Body.String())
		}
	}

	value := askFor(t, refused, http.MethodGet, guarded, "application/json")
	if value.Code != http.StatusForbidden || !strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") ||
		!strings.Contains(value.Body.String(), httpx.CodeDenied) {
		t.Errorf("a client that asked for a value got %d %s %s; it has to keep getting the document with the code in it",
			value.Code, value.Header().Get("Content-Type"), value.Body.String())
	}
}

// TestThePublicWriteLimitRefusesInTheShapeTheVisitorAsked. The public surface is the one
// surface with no account to lock out, so the person this limit answers is the one
// person with no session, no tenant of their own and no log entry of their own to be
// told through. They were told in JSON.
func TestThePublicWriteLimitRefusesInTheShapeTheVisitorAsked(t *testing.T) {
	counted := &window{allow: 0}
	f := guardSetup{allow: true, limiter: counted}
	api, router := guardKernel(t, f, documentFault)
	door := publicly(api, "/ask")

	full := askFor(t, router, http.MethodPost, door, browserAccept)
	if full.Code != http.StatusTooManyRequests {
		t.Fatalf("a public write past the limit = %d, want 429 and not a 500: %s", full.Code, full.Body.String())
	}
	if ct := full.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser past the limit was handed %s: %s", ct, full.Body.String())
	}
	if full.Header().Get("Retry-After") == "" {
		t.Error("the page refuses without saying when to come back")
	}
	if !strings.Contains(full.Body.String(), httpx.CodeLimitExhausted+":") {
		t.Errorf("the refusal names no code a shell can hang a sentence on: %s", full.Body.String())
	}

	value := askFor(t, router, http.MethodPost, door, "application/json")
	if value.Code != http.StatusTooManyRequests ||
		!strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") ||
		!strings.Contains(value.Body.String(), httpx.CodeLimitExhausted) || value.Header().Get("Retry-After") == "" {
		t.Errorf("a client past the limit got %d %s %s; the document and its Retry-After have to survive",
			value.Code, value.Header().Get("Content-Type"), value.Body.String())
	}
}

// TestAnUnresolvableHostIsRefusedInTheShapeAsked. The guards that answer an outage — a
// host the tenant store could not resolve — are the same rule: a person waiting at a
// hostname gets a page that says so, a monitor gets the document it parses. The 503
// carries no code, and this package's own answer for that is the kernel's sentence,
// which is what the page shows.
func TestAnUnresolvableHostIsRefusedInTheShapeAsked(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: true, unresolvable: true}, documentFault)

	page := askFor(t, router, http.MethodGet, at(api, "/plans"), browserAccept)
	if page.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unresolvable host = %d for a browser, want the verdict 503 and not a 500: %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser at a host that will not resolve was handed %s: %s", ct, page.Body.String())
	}
	if page.Header().Get("Retry-After") == "" {
		t.Error("the outage page says nothing about trying again")
	}

	value := askFor(t, router, http.MethodGet, at(api, "/plans"), "application/json")
	if value.Code != http.StatusServiceUnavailable ||
		!strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") {
		t.Errorf("a monitor at the same host got %d %s; a probe has to keep reading a document it can parse",
			value.Code, value.Header().Get("Content-Type"))
	}
}

// TestAnHTMXFormWriteRefusedByAGuardIsTheDocumentItsControllerParses. The generated
// screens write through htmx, and htmx asks with `Accept: text/html,*/*` — so the shape
// rule needs to know that a controller in a page is not a person who navigated.
// ui/assets/js/htmx-config.js swaps nothing for a 4xx and reads the code out of the
// problem body to choose the recovery notice and keep the form's unsaved input; the
// journeys in e2e/session-recovery.spec.ts are that contract being used.
func TestAnHTMXFormWriteRefusedByAGuardIsTheDocumentItsControllerParses(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false}, documentFault)

	req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/plans"), strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/html,*/*") // htmx's own default
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	got := httptest.NewRecorder()
	router.ServeHTTP(got, req)

	if got.Code != http.StatusForbidden {
		t.Fatalf("an htmx write without the grant = %d: %s", got.Code, got.Body.String())
	}
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("an htmx write refused by a guard was handed %s; htmx discards a 4xx body, so the code would reach nobody: %s",
			ct, got.Body.String())
	}
	if !strings.Contains(got.Body.String(), httpx.CodeDenied) {
		t.Errorf("the refusal names no code for the controller to classify: %s", got.Body.String())
	}
}

// TestAGuardRefusalWithNoRendererIsTheDocumentItAlwaysWas. Every one of the guards above
// answers through the same call now, so the default has to be the old answer for all of
// them — an application that registers no renderer is not being asked to render.
func TestAGuardRefusalWithNoRendererIsTheDocumentItAlwaysWas(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false, limiter: &window{allow: 0}}, nil)

	for _, one := range []struct{ name, method, path string }{
		{"the denial", http.MethodGet, at(api, "/plans")},
		{"the public door", http.MethodPost, publicly(api, "/ask")},
	} {
		got := askFor(t, router, one.method, one.path, browserAccept)
		if !strings.HasPrefix(got.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("%s: an application with no renderer answered a browser with %s, want the body it has always answered with",
				one.name, got.Header().Get("Content-Type"))
		}
	}
}
