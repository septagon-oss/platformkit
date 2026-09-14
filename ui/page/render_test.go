package page_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// TestAFragmentIsTheSameHTMLWithoutTheDoctype. A doctype in the middle of a
// page is what a browser does the strangest things with, and htmx swaps what it
// is given straight into one.
func TestAFragmentIsTheSameHTMLWithoutTheDoctype(t *testing.T) {
	t.Parallel()
	node := h.Div(g.Text("3 items"))
	document, err := page.Render(node, http.StatusOK)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	fragment, err := page.RenderFragment(node, http.StatusOK)
	if err != nil {
		t.Fatalf("RenderFragment: %v", err)
	}
	if !strings.HasPrefix(string(document.Body), "<!doctype html>") {
		t.Errorf("the document has no doctype: %s", document.Body)
	}
	if strings.Contains(string(fragment.Body), "doctype") {
		t.Errorf("the fragment carries a doctype: %s", fragment.Body)
	}
	if document.ContentType != fragment.ContentType || fragment.ContentType != httpx.HTMLContentType {
		t.Errorf("the two content types are %q and %q", document.ContentType, fragment.ContentType)
	}
}

// TestAnInlineScriptCarriesThePolicysOwnNonce is the integration InlineScript
// exists for: the nonce on the tag and the nonce in the header are one value,
// per request, so the browser runs the one script this application has. It
// mounts through the real kernel because the nonce is the kernel's.
func TestAnInlineScriptCarriesThePolicysOwnNonce(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: "localhost", Tenants: privacyTenant{}, Conn: conn, Authorize: privacyTenant{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.HTML(api, huma.Operation{
		OperationID: "read-shell", Method: http.MethodGet, Path: "/shell",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*httpx.Page, error) {
		return page.Render(h.HTML(h.Head(page.InlineScript(ctx, `var t=1`))), http.StatusOK)
	})

	seen := map[string]bool{}
	for range 2 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost/shell", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("the shell = %d %s", rec.Code, rec.Body)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		nonce := between(rec.Body.String(), `<script nonce="`, `"`)
		if nonce == "" {
			t.Fatalf("the script carries no nonce: %s", rec.Body)
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
