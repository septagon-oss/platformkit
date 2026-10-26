package httpx_test

// The one negotiation has to hold at the outermost writer too. A file tree is
// mounted on the root router, beside the buffered chain that carries the page
// middleware, so its refusal is written by the API's own answer rather than by a
// handler's — and the person who followed a link to a stylesheet this deployment
// stopped shipping is exactly the reader the kernel used to hand JSON to.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAMissingAssetInAMountedTreeAnswersAPersonWithAPageAndAClientWithTheSameBytesAsAlways(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	api.Surfaces(probe).App.Static("/assets", fstest.MapFS{
		"app.css": &fstest.MapFile{Data: []byte(":root{--a:1}")},
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}

	ask := func(path, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+page(api, path), nil)
		req.Header.Set("Accept", accept)
		req.Header.Set(httpx.RequestIDHeader, traceID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// The file that is there is served: a refusal of a file nobody has is only
	// worth reading when the tree itself answers for the ones it holds.
	if served := ask("/assets/app.css", "text/css"); served.Code != http.StatusOK {
		t.Fatalf("GET the asset the tree holds = %d, want 200: the tree is not mounted as this case assumes", served.Code)
	}

	browser := ask("/assets/absent.css", browserAccept)
	if browser.Code != http.StatusNotFound {
		t.Fatalf("a browser asking for a missing asset got %d, want the verdict 404: %s",
			browser.Code, trim(browser.Body.String()))
	}
	if ct := browser.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser asking for a missing asset was handed %s and not a document: %s",
			ct, trim(browser.Body.String()))
	}
	document := browser.Body.String()
	if !strings.Contains(document, http.StatusText(http.StatusNotFound)) {
		t.Errorf("the page never says what the verdict is called: %s", trim(document))
	}
	if !strings.Contains(document, "Request reference") || !strings.Contains(document, traceID) {
		t.Errorf("the page carries no reference a person can read out to support: %s", trim(document))
	}
	for _, forbidden := range []string{"goroutine", "panic:", "fstest", "absent.css\" "} {
		if strings.Contains(document, forbidden) {
			t.Errorf("the page carries internal detail %q: %s", forbidden, trim(document))
		}
	}

	value := ask("/assets/absent.css", "application/json")
	if value.Code != http.StatusNotFound {
		t.Errorf("the verdict changed for a client that asked for a value: %d, want 404", value.Code)
	}
	if ct := value.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("a client that asked for a value got Content-Type %q", ct)
	}
	if want := problemBody(http.StatusNotFound, "nothing is served at this address"); value.Body.String() != want {
		t.Errorf("the problem document changed shape\n got %q\nwant %q", value.Body.String(), want)
	}
}
