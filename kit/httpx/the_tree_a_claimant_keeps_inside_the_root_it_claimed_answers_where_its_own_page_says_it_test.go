package httpx_test

// The module that claims a surface's root is the one mount whose address nobody
// writes out by hand: it keeps its tree inside the root it was handed, under its own
// name ("/web/assets", the spelling modules/web uses), which is the shape the
// doubling rule refuses everywhere else and the prefix rule spares only because of
// the claim — and Static and PagePath read that rule on separate lines. This asks
// the pairing at that one prefix, and asks the refusal that keeps a claimed root from
// being taken by a tree at all.

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

// TestTheTreeAClaimantKeepsInsideTheRootItClaimedAnswersWhereItsOwnPageSaysIt mounts
// a file tree the way the site's shell does — inside the claimed root, under its own
// module name — and asks it at the address PagePath writes for that same path. The
// bytes come back there, the claimant's name appears exactly once, and the gate keeps
// the root itself with the site: a tree mounted at the claimed root is refused,
// because the file server would stand ahead of every address beneath it.
func TestTheTreeAClaimantKeepsInsideTheRootItClaimedAnswersWhereItsOwnPageSaysIt(t *testing.T) {
	t.Parallel()
	site := func(t *testing.T) (*httpx.API, *httpx.Router, func(string) *httptest.ResponseRecorder) {
		t.Helper()
		store := cache.Memory("pkit-claimant-tree")
		t.Cleanup(func() { _ = store.Close() })
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
		home, claimed := api.Surfaces("web").Public.Home()
		if !claimed {
			t.Fatal("no claimant took the public root, so no shell mounts a tree inside it")
		}
		return api, home, func(path string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
			return w
		}
	}
	tree := fstest.MapFS{
		"app.css":      &fstest.MapFile{Data: []byte(":root{--a:1}")},
		"js/sketch.js": &fstest.MapFile{Data: []byte("export const board = 1")},
	}

	t.Run("the address it names", func(t *testing.T) {
		api, home, ask := site(t)
		home.Static("/web/assets", tree)
		at := home.PagePath("/web/assets")
		if at != "/web/assets" {
			t.Errorf("the claimant of the public root mounts its tree at %q and its page names it at %q, which is not "+
				"where that mount answers: a name doubled or a surface segment added sends every stylesheet and "+
				"controller of the site to an address nobody serves", "/web/assets", at)
		}
		for _, file := range []struct{ rel, body string }{{"/app.css", "--a:1"}, {"/js/sketch.js", "board"}} {
			got := ask(at + file.rel)
			if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), file.body) {
				t.Errorf("the site's page tells the browser to load %q, which answers %d %q", at+file.rel, got.Code, got.Body.String())
			}
		}
		if refused := ask(at + "/absent.css"); refused.Code == http.StatusOK {
			t.Errorf("the claimant's tree answers 200 at %s for a file it does not hold, so this case cannot tell the mount from everything answering", at+"/absent.css")
		}
		if err := api.ValidateDeclarations(); err != nil {
			t.Errorf("a claimant mounting its own tree inside the root it claimed failed the gate: %v", err)
		}
	})

	t.Run("the root it was handed", func(t *testing.T) {
		api, home, ask := site(t)
		home.Static("/", tree)
		// Asked before the gate is consulted: whether a tree mounted at the root
		// answers there is the fact, and the refusal is only the record of it.
		root := ask("/app.css")
		if err := api.ValidateDeclarations(); err == nil {
			t.Errorf("a file tree mounted at the claimed root of the public surface passed the gate that refuses it: "+
				"the root answered %d %q for a file of that tree", root.Code, strings.TrimSpace(root.Body.String()))
			return
		}
		if root.Code == http.StatusOK {
			t.Errorf("the gate refused a file tree at the claimed root and the tree answers there anyway: %d %q",
				root.Code, strings.TrimSpace(root.Body.String()))
		}
	})
}
