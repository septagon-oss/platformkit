package httpx_test

// Every shell names its page's files at one prefix, and both real shells build it
// the same way: they keep one relative path, mount the file tree at it with Static,
// and hand the page the absolute address of that same path with PagePath. The two
// halves are one decision written on two lines, and the cases that ask whether a
// drawing board or an editor reaches a person build a file server of their own at a
// prefix of their own. So this asks the pairing itself, at each shape of mount a
// shell has: were the two ever to come apart, the stylesheet and every controller of
// every page that shell draws would be markup nobody can fetch.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheAddressAPageNamesItsFilesAtIsTheAddressTheMountedTreeAnswers mounts each
// shape of file tree a shell mounts — a module's own on the workspace surface, and a
// public site's on the host root it claimed — and asks files of it at the address
// PagePath composes for the relative path that was mounted, which is the address the
// shell writes into the chrome it hands the page. Files the tree holds, at the root
// and nested, come back with their bytes; a file the tree does not hold is refused,
// so the answers below came from the mount and not from everything answering.
func TestTheAddressAPageNamesItsFilesAtIsTheAddressTheMountedTreeAnswers(t *testing.T) {
	t.Parallel()
	store := cache.Memory("pkit-page-names-its-files")
	defer func() { _ = store.Close() }()
	api, router := httpx.New(httpx.Options{
		Unwired: true, Cache: store, PublicHost: host, Installation: installationHost,
		Log: slog.New(slog.DiscardHandler),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], _ string) (tenancy.Tenant, error) {
			return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
	})
	tree := fstest.MapFS{
		"app.css":          &fstest.MapFile{Data: []byte(":root{--a:1}")},
		"js/sketch.js":     &fstest.MapFile{Data: []byte("export const board = 1")},
		"js/deep/board.js": &fstest.MapFile{Data: []byte("export const deep = 1")},
	}
	home, claimed := api.Surfaces("site").Public.Home()
	if !claimed {
		t.Fatal("the public surface offers no host root to claim, so no site mounts its files at /")
	}
	ask := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
		return w
	}
	for _, mount := range []struct {
		what string
		onto *httpx.Router
	}{
		{"a module's tree on the workspace", api.Surfaces("admin").App},
		{"a site's tree on the host root it claimed", home},
	} {
		at := mount.onto.PagePath("/assets")
		mount.onto.Static("/assets", tree)
		if refused := ask(at + "/js/absent.js"); refused.Code == http.StatusOK {
			t.Errorf("%s answers 200 for a file it does not hold at %s, so this case cannot tell the mount from everything answering",
				mount.what, at+"/js/absent.js")
		}
		for _, file := range []struct{ rel, body string }{
			{"/app.css", "--a:1"}, {"/js/sketch.js", "board"}, {"/js/deep/board.js", "deep"},
		} {
			got := ask(at + file.rel)
			if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), file.body) {
				t.Errorf("the page %s draws tells the browser to load %q, which the mount of that same tree answers %d %q, so its stylesheet and every controller on it are markup nobody can fetch",
					mount.what, at+file.rel, got.Code, got.Body.String())
			}
		}
	}
}
