package httpx_test

// A refusal has one verdict and two shapes. These tests hold the line between them,
// because the failure mode is silent and asymmetric: an API client that suddenly gets a
// page breaks loudly, while a person who gets a JSON blob breaks quietly — they paste it
// into a chat, somebody reads the JSON, and nobody files anything.
//
// Each case asks the same kernel refusal (a cross-site write, and a handler that
// panicked) of a different kind of client, and demands the shape that client can use.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// setupFault is setup for an API that renders its refusals, which is the whole subject.
// The options are otherwise the same ones, so the only variable is the renderer.
func setupFault(t *testing.T, fault httpx.Fault) (*httpx.API, *chi.Mux) {
	t.Helper()
	_, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
		Tenants:      f,
		Conn:         app,
		Authorize:    f,
		Entitle:      f,
		Authenticate: f.authenticate,
		Log:          slog.New(slog.DiscardHandler),
		Fault:        fault,
	})
	httpx.Register(api, huma.Operation{
		OperationID: "write-widget", Method: http.MethodPost, Path: "/widgets",
	}, httpx.Public(), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets/quiet",
	}, httpx.Public(), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "explode-widget", Method: http.MethodPost, Path: "/widgets/explode",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		panic("a handler that fell over")
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return api, router
}

// documentFault is a presentation layer: it answers with a page, and says which verdict
// it was given so the test can insist the status survived the rendering.
func documentFault(w http.ResponseWriter, _ *http.Request, p *problem.Problem) bool {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(p.Status)
	_, _ = w.Write([]byte("<!doctype html><html><body>refused:" + http.StatusText(p.Status) +
		"|" + p.Detail + "|" + p.Instance + "</body></html>"))
	return true
}

// refusingFault is a renderer that declines, which must fall back rather than produce a
// page with nothing in it.
func refusingFault(_ http.ResponseWriter, _ *http.Request, _ *problem.Problem) bool { return false }

func postFrom(t *testing.T, r http.Handler, path, accept, origin string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if origin != "" {
		req.Header.Set("Origin", origin) // a different site: this is what the guard refuses
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// TestABrowserNavigatingToARefusalGetsADocument is the defect this exists for. A
// cross-site write is refused by a guard that runs before any handler, so the person in
// front of the browser saw the problem JSON — correct, and impossible to act on.
func TestABrowserNavigatingToARefusalGetsADocument(t *testing.T) {
	_, router := setupFault(t, documentFault)

	got := postFrom(t, router, "/widgets", browserAccept, "http://elsewhere.test", false)

	if got.Code != http.StatusForbidden {
		t.Errorf("a cross-site write got %d; the page must carry the verdict, not 200 with the word Forbidden in it", got.Code)
	}
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser navigation got Content-Type %q, want a document: %s", ct, got.Body.String())
	}
	body := got.Body.String()
	for _, want := range []string{"refused:Forbidden", "csrf:", "urn:request:"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal page omits %q, so the person reading it has nothing to act on or quote: %s", want, body)
		}
	}
}

// TestAClientThatAskedForValueStillGetsTheProblemDocument is the compatibility half. The
// shapes are chosen by the client, and the existing readers of these bodies are
// machines: an SDK, a health check, a monitor, and every test in this repository that
// reads the JSON. A browser-shaped answer for them would be a quiet outage.
func TestAClientThatAskedForValueStillGetsTheProblemDocument(t *testing.T) {
	_, router := setupFault(t, documentFault)

	cases := map[string]string{
		"an API client": "application/json",
		// curl, probes and monitors: */* is the absence of a preference, not a request
		// to be shown something, and treating it as one answers machines with a page.
		"a probe or curl":  "*/*",
		"no header at all": "",
		// text/html;q=0 is an explicit refusal, not a preference.
		"a client that refused the document": "text/html;q=0, application/json;q=0.9",
	}
	for name, accept := range cases {
		got := postFrom(t, router, "/widgets", accept, "http://elsewhere.test", false)
		if got.Code != http.StatusForbidden {
			t.Errorf("%s: verdict changed to %d while changing shape", name, got.Code)
		}
		if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, problem.ContentType) {
			t.Errorf("%s asked with Accept %q and got %q, want the problem document: %s", name, accept, ct, got.Body.String())
		}
	}
}

// TestAnHTMXRequestGetsTheDocumentItWillSwap: htmx puts whatever arrives into the region
// that asked, which is why the answer to a failed swap should be the markup.
func TestAnHTMXRequestGetsTheDocumentItWillSwap(t *testing.T) {
	_, router := setupFault(t, documentFault)

	got := postFrom(t, router, "/widgets", "*/*", "http://elsewhere.test", true)
	if !strings.HasPrefix(got.Header().Get("Content-Type"), "text/html") {
		t.Errorf("an htmx request got %q: it will swap that into the page", got.Header().Get("Content-Type"))
	}
}

// TestAPanickingHandlerAlsoAnswersABrowserWithADocumentAndNoInternals covers the second
// kernel refusal. The 500's detail is empty on purpose, so a browser still gets a page
// and still gets no hint about what broke.
func TestAPanickingHandlerAlsoAnswersABrowserWithADocumentAndNoInternals(t *testing.T) {
	_, router := setupFault(t, documentFault)

	html := postFrom(t, router, "/widgets/explode", browserAccept, "", false)
	if html.Code != http.StatusInternalServerError {
		t.Errorf("a panicking handler got %d with a document, want 500", html.Code)
	}
	if !strings.Contains(html.Body.String(), "refused:Internal Server Error") {
		t.Errorf("the 500 document does not say what it is: %s", html.Body.String())
	}
	if strings.Contains(html.Body.String(), "a handler that fell over") {
		t.Errorf("the panic's own words reached the browser: %s", html.Body.String())
	}

	json := postFrom(t, router, "/widgets/explode", "application/json", "", false)
	if json.Code != http.StatusInternalServerError || !strings.HasPrefix(json.Header().Get("Content-Type"), problem.ContentType) {
		t.Errorf("the API answer to the same panic changed: %d %q", json.Code, json.Header().Get("Content-Type"))
	}
}

// TestAnApplicationThatRegistersNothingBehavesExactlyAsBefore is why this could be merged
// into a released kernel at all: the default is the old behaviour, byte for byte.
func TestAnApplicationThatRegistersNothingBehavesExactlyAsBefore(t *testing.T) {
	_, router := setupFault(t, nil)

	got := postFrom(t, router, "/widgets", browserAccept, "http://elsewhere.test", false)
	if got.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", got.Code)
	}
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, problem.ContentType) {
		t.Errorf("an application with no renderer got %q, want the problem document it has always got", ct)
	}
}

// TestARendererThatDeclinesFallsBackToTheProblemDocument: opting out for one request has
// to be a real option, or a shell that cannot render some request produces an empty page.
func TestARendererThatDeclinesFallsBackToTheProblemDocument(t *testing.T) {
	_, router := setupFault(t, refusingFault)

	got := postFrom(t, router, "/widgets", browserAccept, "http://elsewhere.test", false)
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, problem.ContentType) {
		t.Errorf("a renderer that declined left %q, want the fallback: %s", ct, got.Body.String())
	}
	if got.Code != http.StatusForbidden {
		t.Errorf("falling back changed the verdict to %d", got.Code)
	}
}

// ask is a request with no session cookie and no body: an address typed into a browser,
// not a form submitted from one.
func ask(t *testing.T, h http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestAMistypedAddressAnswersABrowserWithAPageAndAClientWithAValue. No module handler
// ever sees this request: chi decided nothing matches. Before this, a browser was shown
// net/http's plain-text "404 page not found" — a note written for a developer, which the
// person in front of the window cannot act on and which names no way onward.
func TestAMistypedAddressAnswersABrowserWithAPageAndAClientWithAValue(t *testing.T) {
	_, router := setupFault(t, documentFault)

	page := ask(t, router, http.MethodGet, "/no-such-address", browserAccept)
	if page.Code != http.StatusNotFound {
		t.Fatalf("an unknown address = %d, want 404: %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser at an unknown address got %s, not a page: %s", ct, page.Body.String())
	}
	if body := page.Body.String(); !strings.Contains(body, "refused:Not Found") ||
		!strings.Contains(body, "nothing is served at this address") {
		t.Errorf("the 404 page does not say which verdict it is: %s", body)
	}

	value := ask(t, router, http.MethodGet, "/no-such-address", "")
	if value.Code != http.StatusNotFound {
		t.Errorf("the verdict changed for a client: %d", value.Code)
	}
	if ct := value.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("a client at an unknown address got %s: %s", ct, value.Body.String())
	}
	if !strings.Contains(value.Body.String(), `"status":404`) {
		t.Errorf("the problem document lost its status: %s", value.Body.String())
	}
}

// TestAFormPointedAtTheWrongVerbIsRefusedInBothShapes. A form action that outlived the
// route it pointed at is the other refusal a mux decides alone. The verb is named, because
// "nothing is served here" would send a person to look for a page that is actually there.
func TestAFormPointedAtTheWrongVerbIsRefusedInBothShapes(t *testing.T) {
	_, router := setupFault(t, documentFault)

	page := ask(t, router, http.MethodPost, "/widgets/quiet", browserAccept)
	if page.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a POST to a GET-only address = %d, want 405: %s", page.Code, page.Body.String())
	}
	if body := page.Body.String(); !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "does not accept POST requests") {
		t.Errorf("the 405 is not a page that names the verb: %s", body)
	}

	value := ask(t, router, http.MethodPost, "/widgets/quiet", "")
	if value.Code != http.StatusMethodNotAllowed {
		t.Errorf("the verdict changed for a client: %d", value.Code)
	}
	if !strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") {
		t.Errorf("a client got %s: %s", value.Header().Get("Content-Type"), value.Body.String())
	}
}
