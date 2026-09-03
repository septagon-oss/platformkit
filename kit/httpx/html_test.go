package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// htmlContentType is what every Page carries. The constant itself is not
// exported — nothing outside the package builds a Page by hand — so the two
// assertions below spell it.
const htmlContentType = "text/html; charset=utf-8"

// TestAPageIsAnOperation. The helpers used to live in modules/admin, where a
// client's storefront copied them and the copy drifted: this is the shape both
// of them are now, mounted through the same Register, recorded by the same
// adapter and hidden from the same document.
func TestAPageIsAnOperation(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true

	httpx.HTML(api, huma.Operation{
		OperationID: "read-note", Method: http.MethodGet, Path: "/notes/{id}", Summary: "One note",
	}, httpx.Public(), func(ctx context.Context, in *struct {
		ID string `path:"id"`
	},
	) (*httpx.Page, error) {
		if in.ID == "gone" {
			return nil, httpx.SeeOther("/notes")
		}
		return httpx.Document(h.HTML(h.Body(h.H1(g.Text("Note "+in.ID)))), http.StatusOK)
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the page does not declare its authorization: %v", err)
	}

	res := get(t, router, "/notes/7")
	if res.Code != http.StatusOK {
		t.Fatalf("the page = %d %s", res.Code, res.Body)
	}
	if ct := res.Header().Get("Content-Type"); ct != htmlContentType {
		t.Errorf("the page is served as %q", ct)
	}
	if body := res.Body.String(); !strings.HasPrefix(body, "<!doctype html>") || !strings.Contains(body, "Note 7") {
		t.Errorf("the document is %q", body)
	}

	// A page is not an API: it is out of the OpenAPI document and still on the
	// recording the boot gate reads.
	for _, op := range api.Recorded() {
		if op.OperationID == "read-note" && !op.Hidden {
			t.Error("the page is in the OpenAPI document")
		}
	}

	// A handler saying the answer is elsewhere: a browser is redirected, and
	// htmx — which would swap the target page into a fragment — is told with
	// the header it understands.
	if res := get(t, router, "/notes/gone"); res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/notes" {
		t.Errorf("SeeOther = %d to %q, want 303 to /notes", res.Code, res.Header().Get("Location"))
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/notes/gone", nil)
	req.Header.Set("HX-Request", "true")
	swap := httptest.NewRecorder()
	router.ServeHTTP(swap, req)
	if swap.Code != http.StatusNoContent || swap.Header().Get("HX-Redirect") != "/notes" {
		t.Errorf("SeeOther to htmx = %d with HX-Redirect %q, want 204 to /notes", swap.Code, swap.Header().Get("HX-Redirect"))
	}
}

// TestAFragmentIsTheSameHTMLWithoutTheDoctype. A doctype in the middle of a
// page is what a browser does the strangest things with, and htmx swaps what it
// is given straight into one.
func TestAFragmentIsTheSameHTMLWithoutTheDoctype(t *testing.T) {
	node := h.Div(g.Text("3 items"))
	document, err := httpx.Document(node, http.StatusOK)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	fragment, err := httpx.Fragment(node, http.StatusOK)
	if err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	if !strings.HasPrefix(string(document.Body), "<!doctype html>") {
		t.Errorf("the document has no doctype: %s", document.Body)
	}
	if strings.Contains(string(fragment.Body), "doctype") {
		t.Errorf("the fragment carries a doctype: %s", fragment.Body)
	}
	if document.ContentType != fragment.ContentType || fragment.ContentType != htmlContentType {
		t.Errorf("the two content types are %q and %q", document.ContentType, fragment.ContentType)
	}
}

// TestAnInlineScriptCarriesThePolicysOwnNonce is the integration the helper
// exists for: the nonce on the tag and the nonce in the header are one value,
// per request, so the browser runs the one script this application has.
func TestAnInlineScriptCarriesThePolicysOwnNonce(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true

	httpx.HTML(api, huma.Operation{
		OperationID: "read-shell", Method: http.MethodGet, Path: "/shell",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*httpx.Page, error) {
		return httpx.Document(h.HTML(h.Head(httpx.Script(ctx, `var t=1`))), http.StatusOK)
	})

	seen := map[string]bool{}
	for range 2 {
		res := get(t, router, "/shell")
		csp := res.Header().Get("Content-Security-Policy")
		nonce := between(res.Body.String(), `<script nonce="`, `"`)
		if nonce == "" {
			t.Fatalf("the script carries no nonce: %s", res.Body)
		}
		if !strings.Contains(csp, "'nonce-"+nonce+"'") {
			t.Errorf("the policy %q does not allow the script's nonce %q", csp, nonce)
		}
		seen[nonce] = true
	}
	if len(seen) != 2 {
		t.Error("two requests were served the same nonce; it is per request or it is a constant")
	}
}

func between(s, open, shut string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, shut)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestOnlyALocalPathIsSomewhereToSendABrowser is the open redirect, as a table.
//
// There were two copies of this rule and one of them was wrong: the admin
// sign-in form accepted a leading slash whose second character was not one, and
// `/\evil.example` satisfies that and leaves the site in every browser, because
// they all normalise a backslash to a slash before deciding what the authority
// is. There is one copy now, and this is it.
func TestOnlyALocalPathIsSomewhereToSendABrowser(t *testing.T) {
	for _, tt := range []struct {
		path string
		want bool
	}{
		{"/ok", true},
		{`/\evil.example`, false},       // the one the sign-in form let through
		{"//evil.example", false},       // a network-path reference
		{"////evil.example", false},     // more slashes are still an authority
		{"https://evil.example", false}, // the one every check catches
		{"javascript:alert(1)", false},  // not a path at all
		{"ok", false},                   // relative to whatever page it is on
		{"", false},                     // nowhere
		{"/", true},                     // the home page is a path
		{"/search?q=a#top", true},       // a query and a fragment are part of one
		{`/a\b`, false},                 // a backslash has no meaning in a path
	} {
		if got := httpx.LocalPath(tt.path); got != tt.want {
			t.Errorf("LocalPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
