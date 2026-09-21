package page_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

type privacyTenant struct{}

func (privacyTenant) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

func (privacyTenant) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

func TestSensitivePagesSendPrivacyHeadersBeforeTheirAssets(t *testing.T) {
	_, conn := dbtest.Schema(t)
	kernel, router := httpx.New(httpx.Options{
		PublicHost: "localhost", Tenants: privacyTenant{}, Conn: conn, Authorize: privacyTenant{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			t.Fatal("an anonymous privacy page attempted to authenticate")
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shell := page.Shell{Chrome: chrome(), Back: "/", BackLabel: "Home",
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
	}
	// publicFace is what the kernel answers on the public surface for a page that
	// said nothing about caching itself: cacheable, briefly. The workspace is the
	// one that must never be stored, and a refusal is never stored on either.
	const publicFace = "public, max-age=60"

	for _, tt := range []struct {
		name, policy, cache                  string
		sensitive, hx, localized, revalidate bool
		status                               int
	}{
		{"ordinary", "strict-origin-when-cross-origin", publicFace, false, false, false, false, http.StatusOK},
		{"sensitive", "no-referrer", "no-store", true, false, false, false, http.StatusOK},
		{"refused", "no-referrer", "no-store", true, false, false, false, http.StatusUnprocessableEntity},
		{"redirect", "no-referrer", "no-store", true, false, false, false, http.StatusSeeOther},
		{"htmx-redirect", "no-referrer", "no-store", true, true, false, false, http.StatusNoContent},
		{"localized-ordinary", "strict-origin-when-cross-origin", publicFace, false, false, true, false, http.StatusOK},
		{"localized-sensitive", "no-referrer", "no-store", true, false, true, false, http.StatusOK},
		// A page the owner republishes under the same address: kept, but never
		// answered from without asking. Sensitive still wins over it below.
		{"republished", "strict-origin-when-cross-origin", "no-cache", false, false, false, true, http.StatusOK},
		{"republished-sensitive", "no-referrer", "no-store", true, false, false, true, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := "/" + tt.name
			pageShell := shell
			if tt.localized {
				pageShell.Messages = localeMessages(t, "Account")
			}
			page.Serve(public(kernel), pageShell, page.Route{ID: tt.name, Method: http.MethodGet, Path: path}, httpx.Public(),
				func(context.Context, page.Request, *struct{}) (page.View, error) {
					view := page.View{Title: "Account", Sensitive: tt.sensitive, Revalidate: tt.revalidate}
					if tt.status == http.StatusUnprocessableEntity {
						return view, problem.New(http.StatusUnprocessableEntity, "This link cannot be used.")
					}
					if tt.status == http.StatusSeeOther || tt.hx {
						return view, httpx.SeeOther("/login")
					}
					return view, nil
				})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost"+path+"?token=private-link", nil)
			req.Header.Set("Accept-Language", "pt-PT")
			if tt.hx {
				req.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			response := w.Result()
			defer response.Body.Close()
			if response.StatusCode != tt.status {
				t.Fatalf("page = %d: %s", response.StatusCode, w.Body)
			}
			// Result snapshots the headers emitted before the body. A later meta
			// tag cannot protect the stylesheet and deferred script requests.
			if got := response.Header.Get("Referrer-Policy"); got != tt.policy {
				t.Errorf("Referrer-Policy = %q, want %q", got, tt.policy)
			}
			if got := response.Header.Get("Cache-Control"); got != tt.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.cache)
			}
			if tt.localized && (response.Header.Get("Content-Language") != "pt-PT" || response.Header.Get("Vary") != "Accept-Language") {
				t.Errorf("privacy headers lost negotiated language: %v", response.Header)
			}
			if tt.status == http.StatusSeeOther || tt.hx {
				header := "Location"
				if tt.hx {
					header = "HX-Redirect"
				}
				if got := response.Header.Get(header); got != "/login" {
					t.Errorf("%s = %q, want /login", header, got)
				}
				return
			}
			for _, asset := range []string{`rel="stylesheet"`, `<script src="/admin/assets/js/htmx.min.js" defer>`} {
				if !strings.Contains(w.Body.String(), asset) {
					t.Errorf("page has no early asset %q", asset)
				}
			}
		})
	}
}
