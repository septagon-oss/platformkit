package screens_test

// A year is a promise with two halves. The first is that an address naming these
// bytes may be kept: the cases beside this one ask that, and that the query a page
// writes is the fingerprint the mount answers. The second is what a kept copy is
// worth, and no case here asks it: when the browser does come back — a hard reload,
// a returning tab, a cache the operating system trimmed — the mount has to answer
// with 304 for the bytes it holds and with the file for any other. A 304 is the one
// answer that leaves a browser holding whatever copy it already has, so it is safe
// only while the validator names the bytes; a fingerprint of something else would
// keep a stale sheet in place with no download left to notice.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// TestAGuidedSheetAnswersItsYearWithARevalidationOfItsOwn buys the sheet address a
// guided page writes, then returns to it as a browser that kept it would: with the
// ETag of those bytes, which has to buy a 304 and no body, and with a validator of
// other bytes, which has to reach the file and carry it. A mount that answers 304 to
// any validator, and one that never revalidates so that every return pays a full
// download, both refuse here.
func TestAGuidedSheetAnswersItsYearWithARevalidationOfItsOwn(t *testing.T) {
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

	view := core.Guided(core.GuidedPage{Session: session, Policy: core.GuidedPolicy{
		Complete: core.GuidedCapability{Declared: true, Callable: true},
		Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
		Base: "/app/note/notes/" + session.ID.String(), Exit: "/app", Mode: "step", Step: 1, Extra: 15},
		core.Options{})
	bought := sheetTheWalkPaysFor.FindStringSubmatch(writeDocument(t, page.Document(chrome, page.Request{Path: "step"}, view, g.Group(view.Body))))
	if bought == nil {
		t.Fatal("the step document writes no stylesheet address with a ?v=, so nothing here has a year to revalidate")
	}
	address, query := bought[1], bought[2]

	first := buy(t, router, address+"?v="+query)
	if first.Code != http.StatusOK || first.Header().Get("ETag") != `"`+query+`"` {
		t.Fatalf("the step sheet at %s?v=%s answers %d with ETag %q, want 200 and the query itself as the validator", address, query, first.Code, first.Header().Get("ETag"))
	}

	still := ask(t, router, address+"?v="+query, "If-None-Match", `"`+query+`"`)
	if still.Code != http.StatusNotModified {
		t.Errorf("the browser that kept %s?v=%s comes back with its validator and is answered %d, want 304: the year it was sold is worth nothing if every return pays a download", address, query, still.Code)
	}
	if len(still.Body.Bytes()) != 0 {
		t.Errorf("the revalidation of %s answers %d bytes, want none: a 304 that carries a body is a download wearing a revalidation", address, still.Body.Len())
	}

	other := ask(t, router, address+"?v="+query, "If-None-Match", `"8888888888888888"`)
	if other.Code != http.StatusOK {
		t.Errorf("the sheet answers a validator naming bytes it does not carry with %d: a browser holding some other file has to be given this one", other.Code)
	}
	if got := other.Body.String(); got != string(sheet.Body) {
		t.Errorf("the sheet answers a stale validator with %d bytes, not the %d of the composed sheet", len(got), len(sheet.Body))
	}
}

// ask fetches one address with one header set, as a browser that kept a copy would.
func ask(t *testing.T, router http.Handler, address, key, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+address, nil)
	req.Header.Set(key, value)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
