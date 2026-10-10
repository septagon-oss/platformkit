package page_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// wasmToken is the directive a page gains for itself when its view says the
// document instantiates WebAssembly (document.View.RunsWASM). It is spelled here
// rather than read from kit/httpx because the point of the pair below is that the
// adapter reaches the kernel's declaration and nothing else: a page that asked for
// it gets this token, and a page that did not gets the policy it has always got.
const wasmToken = " 'wasm-unsafe-eval'"

// wasmShell is the fixture every case here mounts through: the chrome of the other
// page tests, with a frame that adds nothing, so the document is the shell's own
// and its inline theme script carries the request's nonce.
func wasmShell() page.Shell {
	return page.Shell{Chrome: chrome(), Back: "/", BackLabel: "Home",
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}
}

func wasmKernel(t *testing.T) (*httpx.API, http.Handler) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	kernel, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: "localhost", Tenants: privacyTenant{}, Conn: conn, Authorize: privacyTenant{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	return kernel, router
}

// TestAViewThatRunsWASMDeterminesItsOwnPolicy is the adapter half of the door: the
// field a module author writes against reaches the kernel's declaration, and it
// reaches it for the document that was rendered — not for a refusal, not for a
// redirect, and not for the page beside it that said nothing.
func TestAViewThatRunsWASMDeterminesItsOwnPolicy(t *testing.T) {
	kernel, router := wasmKernel(t)
	const publicFace = "public, max-age=60" // what the public surface answers for a page that said nothing about caching

	for _, tt := range []struct {
		name                  string
		runs                  bool
		hx                    bool
		status                int
		want                  bool // the response carries the token
		doc                   bool // the response is a document, so there is a policy and a nonce to read
		cache, referrerPolicy string
	}{
		{"plain", false, false, http.StatusOK, false, true, publicFace, "strict-origin-when-cross-origin"},
		{"editor", true, false, http.StatusOK, true, true, publicFace, "strict-origin-when-cross-origin"},
		// The handler refused: what a person is shown is the shell's fault page,
		// which instantiates nothing, so the declaration the refused view carried is
		// dropped with the body it was about.
		{"refused-editor", true, false, http.StatusUnprocessableEntity, false, true, "no-store", "strict-origin-when-cross-origin"},
		// A redirect and htmx's version of one: no document, no policy to widen. The
		// caching is the surface's own rule, unchanged by the declaration: a 303 is no
		// success to store, a 204 answered to a safe method is (headers.go).
		{"redirect-editor", true, false, http.StatusSeeOther, false, false, "no-store", "strict-origin-when-cross-origin"},
		{"htmx-redirect-editor", true, true, http.StatusNoContent, false, false, publicFace, "strict-origin-when-cross-origin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := "/" + tt.name
			page.Serve(public(kernel), wasmShell(), page.Route{ID: tt.name, Method: http.MethodGet, Path: path}, httpx.Public(),
				func(context.Context, page.Request, *struct{}) (page.View, error) {
					v := page.View{Title: "Editor", RunsWASM: tt.runs, Body: []g.Node{g.Text("draw")}}
					if tt.status == http.StatusUnprocessableEntity {
						return v, problem.New(http.StatusUnprocessableEntity, "That image cannot be opened.")
					}
					if tt.status == http.StatusSeeOther || tt.hx {
						return v, httpx.SeeOther("/login")
					}
					return v, nil
				})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost"+path, nil)
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
			if got := response.Header.Get("Cache-Control"); got != tt.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.cache)
			}
			if got := response.Header.Get("Referrer-Policy"); got != tt.referrerPolicy {
				t.Errorf("Referrer-Policy = %q, want %q", got, tt.referrerPolicy)
			}
			csp := response.Header.Get("Content-Security-Policy")
			if !tt.doc {
				header := "Location"
				if tt.hx {
					header = "HX-Redirect"
				}
				if got := response.Header.Get(header); got != "/login" {
					t.Errorf("%s = %q, want /login", header, got)
				}
				if csp != "" {
					t.Errorf("a response that is no document carries the policy %q", csp)
				}
				return
			}
			if got := strings.Contains(csp, wasmToken); got != tt.want {
				t.Errorf("the policy %q carries the token: %v, want %v", csp, got, tt.want)
			}
			if !tt.want {
				return
			}
			// The shell's own theme script is the inline script every document of
			// this application carries, so a page that widened its policy by asking
			// for WASM has to leave that allowance standing or it breaks the chrome
			// it lives in. It is the failure the old storefront copy actually had.
			nonce := between(w.Body.String(), `<script nonce="`, `"`)
			if nonce == "" {
				t.Fatalf("the shell's inline script carries no nonce: %s", w.Body)
			}
			if src := strings.SplitN(strings.SplitN(csp, "script-src ", 2)[1], ";", 2)[0]; src != "'self'"+wasmToken+" 'nonce-"+nonce+"'" {
				t.Errorf("script-src is %q, want 'self', the token and this request's nonce", src)
			}
		})
	}
}

// TestAViewThatRunsWASMDeterminesNothingAboutItsMarkup: the declaration is a
// response header and not a second hand on the document. A view that declared and
// one that did not render the same bytes, which is what leaves the shell, the
// design system and every localized page the single owner of what a page says.
func TestAViewThatRunsWASMDeterminesNothingAboutItsMarkup(t *testing.T) {
	kernel, router := wasmKernel(t)
	for _, name := range []string{"declared", "silent"} {
		page.Serve(public(kernel), wasmShell(), page.Route{ID: name, Method: http.MethodGet, Path: "/" + name}, httpx.Public(),
			func(context.Context, page.Request, *struct{}) (page.View, error) {
				return page.View{Title: "Editor", RunsWASM: name == "declared", Body: []g.Node{g.Text("draw")}}, nil
			})
	}
	body := func(at string) (string, string) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost"+at, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", at, w.Code, w.Body)
		}
		return w.Body.String(), between(w.Body.String(), `<script nonce="`, `"`)
	}
	declared, declaredNonce := body("/declared")
	silent, silentNonce := body("/silent")
	if declaredNonce == "" || silentNonce == "" || declaredNonce == silentNonce {
		t.Fatalf("the two documents carry %q and %q; a nonce is per request", declaredNonce, silentNonce)
	}
	// The nonce is the one thing two requests cannot share, so it is the one thing
	// put back before the bytes are compared whole.
	if got := strings.ReplaceAll(declared, declaredNonce, silentNonce); got != silent {
		t.Errorf("the declaration changed the markup: %s and %s differ once the nonce is the same", declared, silent)
	}
}
