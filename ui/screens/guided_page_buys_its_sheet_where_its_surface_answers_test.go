package screens_test

// A guided page is unreadable without its stylesheet, and the address it writes is a
// promise with two halves: that the assets prefix it names is where the surface serving
// that page also serves the file, and that the query on it names the bytes behind it,
// which is what buys the sheet a year in the browser's cache. The cases beside this one
// each hold one half — one reads the fingerprint out of the written document, one reads
// the tree the shell was handed, one reads what a kernel file tree answers at an
// address — and none joins them the way a browser does. A composition that mounts its
// assets under one prefix and hands page.Chrome another, or builds Chrome.Assets by hand
// instead of asking the router for the page address, keeps every element, class and hash
// in place while every step of a walk downloads a sheet that answers 404. So this case
// mounts the tree on the surface, writes each guided page against the address that
// surface answers at, and buys what those bytes told the browser to buy.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	core "github.com/septagon-oss/platformkit/ui/resource"
)

// sheetTheWalkBuys is the stylesheet address a guided document writes, path and `?v=`
// kept apart: the half the walk obeys is the whole address, query included.
var sheetTheWalkBuys = regexp.MustCompile(`<link rel="stylesheet" href="([^"?]+)\?v=([^"]*)"`)

// TestAGuidedPageBuysItsSheetWhereItsSurfaceAnswers mounts the sheet a guided session
// composes on the App surface, writes each page of a walk with that surface's own page
// address as the chrome's assets prefix, and fetches the address each written page tells
// the browser to fetch. The sheet has to arrive with the year the static rule gives an
// address that names its bytes, and the same file at an address that names nothing has to
// be revalidated — the contrast is what shows the query the document writes is the thing
// being bought. A page whose address the surface does not answer, one that answers without
// the year, and a chrome whose prefix is not the mount's page address each refuse per page.
func TestAGuidedPageBuysItsSheetWhereItsSurfaceAnswers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	words, session := mountWords(t), oneSetRecorded()
	sheet := ui.Compose(design.Default())
	app := api.Surfaces("note").App
	app.Static("assets", ui.Assets(sheet))
	chrome := page.Chrome{Brand: "Practice", SignIn: "/app/login", Assets: app.PagePath("assets"), Stylesheet: sheet}

	for _, screen := range []struct {
		mode      string
		step, set int
	}{
		{"step", 1, 0}, {"rest", 0, 0}, {"finish", 0, 0}, {"summary", 0, 0},
	} {
		view := core.Guided(core.GuidedPage{Session: session, Policy: core.GuidedPolicy{
			Complete: core.GuidedCapability{Declared: true, Callable: true},
			Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
			Base: "/app/note/notes/" + session.ID.String(), Exit: "/app", Mode: screen.mode,
			Step: screen.step, Set: screen.set, Extra: 15},
			core.Options{})
		out := writeDocument(t, page.Document(chrome, page.Request{Path: screen.mode}, view, g.Group(view.Body)))
		bought := sheetTheWalkBuys.FindStringSubmatch(out)
		if bought == nil {
			t.Fatalf("the %s document writes no stylesheet address with a ?v=, so the person holding it has nothing to buy at the mount", screen.mode)
		}
		if kept := buy(t, router, bought[1]+"?v="+bought[2]); kept.Code != http.StatusOK {
			t.Fatalf("the %s document buys its sheet at %s and the mount answers %d", screen.mode, bought[1], kept.Code)
		} else if got := kept.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("the %s document buys %s, which the mount serves as %q: the query that page writes does not name the bytes this mount answers, so every step of the walk asks for the sheet again",
				screen.mode, bought[1]+"?v="+bought[2], got)
		}
		if again := buy(t, router, bought[1]); again.Code != http.StatusOK {
			t.Errorf("the %s sheet answers %d at its bare address; a step of the walk that revalidates needs the file", screen.mode, again.Code)
		} else if got := again.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("the %s sheet at its bare address says %q, want %q: the year belongs to the address that names the bytes and to no other",
				screen.mode, got, "no-cache")
		}
	}
}

// buy fetches one address from the mounted router as a browser would, with the session the
// composition's Authenticate answers for every request.
func buy(t *testing.T, router http.Handler, address string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+address, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
