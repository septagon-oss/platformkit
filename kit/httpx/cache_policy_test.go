package httpx_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// cacheSaying registers a handler on r that answers 200 with the Cache-Control it is
// given, the way a module that meant well would.
func cacheSaying(r *httpx.Router, id, path, value string) {
	httpx.Register(r, huma.Operation{OperationID: id, Method: http.MethodGet, Path: path}, httpx.Public(),
		func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				hctx.SetHeader("Content-Type", "application/json")
				hctx.SetHeader("Cache-Control", value)
				hctx.SetStatus(http.StatusOK)
				_, _ = hctx.BodyWriter().Write([]byte(`{}`))
			}}, nil
		})
}

// TestASessionSurfaceNeverAnswersSomethingACacheMayKeep is docs/cache.md's first row. A
// workspace answer is somebody's own, so a handler may add to no-store and never take it
// away: `public, max-age=3600` from a module on the App surface would let a shared cache
// hand one tenant's answer to the next person through, and it is replaced. A value that
// already forbids storing is the handler's to keep, and the public face keeps whatever
// its handler says, because there the handler knows and the kernel does not.
func TestASessionSurfaceNeverAnswersSomethingACacheMayKeep(t *testing.T) {
	api, router, _ := setup(t)
	surfaces := api.Surfaces(probe)
	cacheSaying(surfaces.App, "loose", "/loose", "public, max-age=3600")
	cacheSaying(surfaces.App, "strict", "/strict", "private, no-store")
	cacheSaying(surfaces.Public, "face", "/face", "public, max-age=600")

	if got := get(t, router, at(api, "/loose")).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("an App handler's `public, max-age=3600` reached the client as %q; a session surface answers no-store", got)
	}
	if got := get(t, router, at(api, "/strict")).Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("an App handler that already forbids storing was overwritten: %q", got)
	}
	if got := get(t, router, surfaces.Public.Path("/face")).Header().Get("Cache-Control"); got != "public, max-age=600" {
		t.Errorf("the public face's own caching was overwritten: %q", got)
	}
}

// TestAKernelAssetIsImmutableAtItsFingerprintAndRevalidatedElsewhere is docs/cache.md's
// static row. The stylesheet is the same bytes for every person, so the session rule does
// not apply to it: at the address that names its content (`?v=` is the first eight bytes of
// its SHA-256, as ui.Sheet computes them) it may be kept for a year, anywhere else it may be
// kept and must be revalidated, and a revalidation that finds it unchanged is a 304.
func TestAKernelAssetIsImmutableAtItsFingerprintAndRevalidatedElsewhere(t *testing.T) {
	api, router, _ := setup(t)
	body := []byte(":root{--x:1}")
	api.Surfaces(probe).App.Static("/assets", fstest.MapFS{"app.css": &fstest.MapFile{Data: body}})
	sum := sha256.Sum256(body)
	tag := hex.EncodeToString(sum[:8])

	css := "/app/" + probe + "/assets/app.css" // a page tree answers under the surface's page namespace
	pinned := get(t, router, css+"?v="+tag)
	if got := pinned.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("the stylesheet at its fingerprint says %q about caching", got)
	}
	if got := pinned.Header().Get("ETag"); got != `"`+tag+`"` {
		t.Errorf("the stylesheet's ETag is %q, want its content %q", got, tag)
	}

	if pinned.Code != http.StatusOK {
		t.Fatalf("the stylesheet at %s answered %d", css, pinned.Code)
	}

	for _, address := range []string{css, css + "?v=stale"} {
		res := get(t, router, address)
		if got := res.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s says %q; an address that does not name the content is kept only with revalidation", address, got)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "http://"+host+css, nil)
	req.Header.Set("If-None-Match", `"`+tag+`"`)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotModified {
		t.Errorf("a revalidation that names the current content answered %d, want 304", res.Code)
	}
}
