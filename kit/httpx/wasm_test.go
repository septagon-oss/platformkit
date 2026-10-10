package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// The four policies below are the whole of what this file is about, spelled out
// rather than derived from the constant under test: a page that declares it runs
// WebAssembly is served the one carrying the token, and every other page is
// served the one it has always been served. Deriving either from kit/httpx would
// let the pair drift together with the policy and report that nothing changed,
// which is the failure the second half of the acceptance exists to catch. "%" is
// the request nonce, which the middleware substitutes.
const (
	appPolicy = "default-src 'self'; script-src 'self' 'nonce-%'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
	appPolicyWASM = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval' 'nonce-%'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
	publicPolicy = "default-src 'self'; script-src 'self' 'nonce-%'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	publicPolicyWASM = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval' 'nonce-%'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
)

// wasmTag is the one inline script these documents carry. It is written by hand
// because the kernel renders nothing: the tag is here so a response names a nonce
// the body actually uses, which is the pair a browser checks.
func wasmTag(ctx context.Context) []byte {
	return []byte(`<!doctype html><html><body><script nonce="` + httpx.Nonce(ctx) + `">1</script></body></html>`)
}

// wasmPage mounts one document that either declares WebAssembly or says nothing,
// at the address it is given. Declaring twice is part of the mount: a render path
// that crosses the declaration twice should read as one declaration.
func wasmPage(r *httpx.Router, id, path string, declares bool) {
	httpx.HTML(r, huma.Operation{
		OperationID: id, Method: http.MethodGet, Path: path,
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*httpx.Page, error) {
		if declares {
			httpx.AllowWASM(ctx)
			httpx.AllowWASM(ctx)
		}
		return &httpx.Page{Status: http.StatusOK, ContentType: httpx.HTMLContentType, Body: wasmTag(ctx)}, nil
	})
}

// nonceOf is the nonce the document's own tag carries — the value the policy has
// to name, and the one thing two responses of one route differ in.
func nonceOf(t *testing.T, res *httptest.ResponseRecorder) string {
	t.Helper()
	body := res.Body.String()
	_, after, ok := strings.Cut(body, `<script nonce="`)
	if !ok {
		t.Fatalf("the document carries no tagged script: %s", body)
	}
	n, _, _ := strings.Cut(after, `"`)
	if n == "" {
		t.Fatalf("the document carries an empty nonce: %s", body)
	}
	return n
}

// cspOf is the response's policy, checked whole against a golden with this
// response's nonce put in it.
func cspOf(t *testing.T, res *httptest.ResponseRecorder, golden string) string {
	t.Helper()
	got := res.Header().Get("Content-Security-Policy")
	if want := strings.Replace(golden, "%", nonceOf(t, res), 1); got != want {
		t.Errorf("policy = %q, want %q", got, want)
	}
	return got
}

// TestADeclaredWASMPageIsServedItsOwnScriptPolicy is the first half of the
// acceptance: the page that declared carries 'wasm-unsafe-eval' inside script-src
// and keeps its nonce, and the page beside it that said nothing is served the
// policy it was served before this declaration existed, character for character.
func TestADeclaredWASMPageIsServedItsOwnScriptPolicy(t *testing.T) {
	api, router, _ := setup(t)
	wasmPage(api.Surfaces(probe).App, "read-editor", "/editor", true)
	wasmPage(api.Surfaces(probe).App, "read-list", "/list", false)

	flagged := get(t, router, page(api, "/editor"))
	if flagged.Code != http.StatusOK {
		t.Fatalf("the page = %d %s", flagged.Code, flagged.Body)
	}
	csp := cspOf(t, flagged, appPolicyWASM)
	// The token sits in script-src and nowhere else: a policy that gained it in a
	// clause nobody named allows more than the page asked for.
	if src := strings.SplitN(strings.SplitN(csp, "script-src ", 2)[1], ";", 2)[0]; src != "'self' 'wasm-unsafe-eval' 'nonce-"+nonceOf(t, flagged)+"'" {
		t.Errorf("script-src is %q, want 'self', the token and this request's nonce", src)
	}
	// The nonce is what the historical failure lost: a page that widened its own
	// policy by hand wrote the token, dropped the tag's allowance, and the browser
	// then refused the one script the shell cannot do without.
	if !strings.Contains(csp, "'nonce-"+nonceOf(t, flagged)+"'") {
		t.Errorf("the policy %q does not name the nonce the document carries", csp)
	}
	if strings.Contains(csp, "%") {
		t.Errorf("the policy %q carries an unsubstituted nonce", csp)
	}

	// The rest of the response is the one every authenticated document gets: a
	// declaration is a directive and not a licence to be cached, framed or indexed.
	h := flagged.Header()
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("the flagged document says %q about caching, want no-store", got)
	}
	if got := h.Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("the flagged document says %q about indexing", got)
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("the flagged document may be framed: %q", got)
	}
	if got := h.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("the flagged document leaks its URL: %q", got)
	}
	if got := h.Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("the flagged document carries %q, want an HSTS policy at a public host", got)
	}

	// And the page that said nothing is byte-identical to today's.
	silent := get(t, router, page(api, "/list"))
	if plain := cspOf(t, silent, appPolicy); strings.Contains(plain, "wasm") {
		t.Errorf("a page that declared nothing was served %q", plain)
	}
}

// TestTheWASMDeclarationChangesOnlyWhatItDeclares asks the same pair on the public
// face, which is the other place a page may live and a third shape of caching and
// indexing. What it asks of both faces is that the difference between a flagged
// policy and an unflagged one is the token and nothing else — not the surface's
// object-src, not the ordering, not the spacing.
func TestTheWASMDeclarationChangesOnlyWhatItDeclares(t *testing.T) {
	api, router, _ := setup(t)
	public := api.Surfaces(probe).Public
	wasmPage(public, "read-editor-face", "/editor", true)
	wasmPage(public, "read-list-face", "/list", false)
	wasmPage(api.Surfaces(probe).App, "read-editor", "/editor", true)
	wasmPage(api.Surfaces(probe).App, "read-list", "/list", false)

	for _, face := range []struct{ flagged, silent, flaggedGolden, silentGolden string }{
		{public.PagePath("/editor"), public.PagePath("/list"), publicPolicyWASM, publicPolicy},
		{page(api, "/editor"), page(api, "/list"), appPolicyWASM, appPolicy},
	} {
		flagged := get(t, router, face.flagged)
		silent := get(t, router, face.silent)
		csp := cspOf(t, flagged, face.flaggedGolden)
		if got, was := flagged.Header().Get("X-Robots-Tag"), silent.Header().Get("X-Robots-Tag"); got != was {
			t.Errorf("%s and %s are indexed differently: %q against %q", face.flagged, face.silent, got, was)
		}
		if got, was := flagged.Header().Get("Cache-Control"), silent.Header().Get("Cache-Control"); got != was {
			t.Errorf("%s and %s are cached differently: %q against %q", face.flagged, face.silent, got, was)
		}
		// Take the token back out and the two policies are one string. This is the
		// golden's claim stated without a nonce in the way: the flag is the whole of
		// the difference, in one place. The deletion only answers the silent policy if
		// the token sat where it was put — between two spaces, inside script-src — so a
		// token that landed in another clause, or that arrived without its spaces,
		// fails here instead of matching some golden by accident.
		if same := strings.Replace(csp, " 'wasm-unsafe-eval'", "", 1); same == csp {
			t.Errorf("removing the token from %q changed nothing, so it is not spelled between two spaces", csp)
		} else if want := strings.Replace(face.silentGolden, "%", nonceOf(t, flagged), 1); same != want {
			t.Errorf("the flag is more than a token: %q without it is %q, want the unflagged policy %q", csp, same, want)
		}
	}
}

// TestTheWASMDeclarationNeverTravels: the claim belongs to one response. It is
// asked concurrently on purpose — a flag kept on the API, on the surface or in a
// package variable answers this under -race as readily as in sequence, and a shared
// flag that only showed itself under load is the failure worth having here.
func TestTheWASMDeclarationNeverTravels(t *testing.T) {
	api, router, _ := setup(t)
	wasmPage(api.Surfaces(probe).App, "read-editor", "/editor", true)
	wasmPage(api.Surfaces(probe).App, "read-list", "/list", false)

	var wg sync.WaitGroup
	answers := make([]string, 16)
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			at, golden := page(api, "/list"), appPolicy
			if i%2 == 0 {
				at, golden = page(api, "/editor"), appPolicyWASM
			}
			res := get(t, router, at)
			got := res.Header().Get("Content-Security-Policy")
			if want := strings.Replace(golden, "%", nonceOf(t, res), 1); got != want {
				answers[i] = at + " was served " + got + ", want " + want
			}
		}(i)
	}
	wg.Wait()
	for _, a := range answers {
		if a != "" {
			t.Error(a)
		}
	}
}

// TestAPageThatOwnsItsPolicyKeepsItEvenWhenItDeclares. An isolated preview or a
// file download writes a policy of its own, usually a stricter one, and the
// middleware declines to overwrite it (headers.go). A declaration may widen the
// kernel's default and never a handler's own: the escape hatch stays one-way.
func TestAPageThatOwnsItsPolicyKeepsItEvenWhenItDeclares(t *testing.T) {
	api, router, _ := setup(t)
	httpx.HTML(api.Surfaces(probe).App, huma.Operation{
		OperationID: "read-preview", Method: http.MethodGet, Path: "/preview",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*httpx.Page, error) {
		httpx.AllowWASM(ctx)
		return &httpx.Page{Status: http.StatusOK, ContentType: httpx.HTMLContentType,
			ContentSecurityPolicy: "default-src 'none'; sandbox", Body: wasmTag(ctx)}, nil
	})
	if got := get(t, router, page(api, "/preview")).Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("the handler's own policy became %q", got)
	}
}

// TestAWASMDeclarationAfterTheFirstByteChangesNothing states the limit instead of
// leaving it to be discovered: the policy is composed with the status, so a page
// that streamed past the response buffer keeps the policy it was given and its
// WebAssembly.instantiate stays refused. Nothing is corrupted — the response is
// still its document, with the header it already had.
func TestAWASMDeclarationAfterTheFirstByteChangesNothing(t *testing.T) {
	api, router, _ := setup(t)
	// Registered rather than mounted with HTML for the same reason the streaming
	// probe in headers_test.go is: the point is a response whose bytes went before
	// the declaration, and only a streamed body says that.
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "stream-editor", Method: http.MethodGet, Path: "/stream",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", httpx.HTMLContentType)
			hctx.SetStatus(http.StatusOK)
			_, _ = hctx.BodyWriter().Write(wasmTag(ctx))
			// Flush is the moment an answer stops being rewriteable. Until it the
			// whole response is held (buffer.go), so a declaration after a write and
			// before the flush is still heard — which is what the ordinary path is.
			if w, writable := hctx.BodyWriter().(http.ResponseWriter); writable {
				_ = http.NewResponseController(w).Flush()
			}
			httpx.AllowWASM(ctx)
		}}, nil
	})
	res := get(t, router, at(api, "/stream"))
	if res.Code != http.StatusOK {
		t.Fatalf("the stream = %d %s", res.Code, res.Body)
	}
	if got, want := res.Header().Get("Content-Security-Policy"), strings.Replace(appPolicy, "%", nonceOf(t, res), 1); got != want {
		t.Errorf("a declaration after the first byte wrote %q, want the policy the response already had: %q", got, want)
	}
}

// TestAWASMDeclarationOutsideAResponseIsHeardByNobody: the declaration is a claim
// about a response, and with no response there is nothing to claim about. A page
// rendered by a CLI, a job or a unit test that reaches for it must not panic, any
// more than httpx.Nonce outside a request does.
func TestAWASMDeclarationOutsideAResponseIsHeardByNobody(t *testing.T) {
	httpx.AllowWASM(t.Context())
	if got := httpx.Nonce(t.Context()); got != "" {
		t.Fatalf("the nonce outside a request is %q, which is the shape this declaration copies", got)
	}
}
