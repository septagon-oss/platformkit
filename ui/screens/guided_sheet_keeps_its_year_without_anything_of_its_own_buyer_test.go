package screens_test

// A guided page sells its stylesheet for a year, and the year is stated on an answer that
// any cache may keep: `public, max-age=31536000, immutable`, on the workspace surface, where
// everything else the surface answers is `no-store` precisely because a shared cache holding
// one person's answer hands it to the next. The asset row earns that exception by being the
// same bytes for whoever asks, so two things have to stay true of it together, and the cases
// beside this one ask only one of them: what a kept copy is answered with (a 304 naming the
// bytes it holds), and nothing about what that answer says or carries. A revalidation whose
// 304 stopped restating the year is a copy a browser drops and re-downloads at every step of
// a walk, and an answer every cache may keep for a year that carries a person's own
// credential is that person's credential handed to whoever asks the address next. So this
// case reads the address a step document writes, and asks of both halves of its year — the
// first answer and the revalidation of the copy that answer sold — that they name one
// validator, say one caching policy, and carry nothing of the person who asked.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
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

// keptForAYear is the answer's half of the promise an asset address is kept for: the
// caching policy of docs/cache.md's static row, stated on the first answer and restated on
// every revalidation of it, because a 304 replaces the stored answer's headers rather than
// leaving them alone.
const keptForAYear = "public, max-age=31536000, immutable"

// TestAGuidedSheetKeepsItsYearWithoutAnythingOfItsOwnBuyer buys the sheet address a guided
// step writes and returns to it as the browser that kept that answer would, and asks of both
// answers: that the year they sell is the one policy of the static row and the same on each,
// that the validator is the same on each and is the fingerprint the address names, and that
// neither answer sets a cookie or names the person who asked. A mount that answers a
// revalidation with no policy, one that answers it with the surface's `no-store`, and one
// that mints a session cookie on the way to a file are each refused here.
func TestAGuidedSheetKeepsItsYearWithoutAnythingOfItsOwnBuyer(t *testing.T) {
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
		t.Fatal("the step document writes no stylesheet address with a ?v=, so nothing here has a year to keep")
	}
	address, query := bought[1]+"?v="+bought[2], bought[2]

	first, kept := buy(t, router, address), ask(t, router, address, "If-None-Match", `"`+query+`"`)
	if kept.Code != http.StatusNotModified {
		t.Fatalf("the kept copy came back and was answered %d, want 304: the year the page was sold is worth nothing otherwise", kept.Code)
	}
	for _, answer := range []struct {
		what   string
		code   int
		header http.Header
	}{
		{"first answer", first.Code, first.Header()},
		{"the revalidation of the copy it sold", kept.Code, kept.Header()},
	} {
		if got := answer.header.Get("Cache-Control"); got != keptForAYear {
			t.Errorf("the sheet's %s says %q about caching, want %q: an address kept for a year has to say so on every answer about it, or the browser drops the copy it holds and pays for the sheet again",
				answer.what, got, keptForAYear)
		}
		if got := strings.Trim(answer.header.Get("ETag"), `"`); got != query {
			t.Errorf("the sheet's %s names the validator %q, not the %q its address was bought with: the year and the revalidation have to speak of one file",
				answer.what, got, query)
		}
		// The header a cache would store beside a year-old copy of a workspace answer.
		if got := answer.header.Values("Set-Cookie"); len(got) != 0 {
			t.Errorf("the sheet's %s sets %d cookie(s) on an answer every cache may keep for a year, so whoever asks that address next is handed the person who asked first: %v",
				answer.what, len(got), got)
		}
	}
}
