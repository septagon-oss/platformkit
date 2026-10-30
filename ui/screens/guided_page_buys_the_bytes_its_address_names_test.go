package screens_test

// The year a guided page buys for its sheet is a promise about bytes, not about an
// address. Two packages hash independently: `ui.Sheet` puts the first eight bytes of the
// SHA-256 of the sheet a caller composed into the `?v=` of the link every page writes,
// and the kernel's static rule answers the year only where a request's `?v=` names the
// first eight bytes of the SHA-256 of the body the file server is about to write. The two
// figures agree only while the composed sheet and the served file are the same bytes, and
// the cases beside this one ask that the address a document writes is answered and kept
// for the year — never that the year is keyed to what arrives. So this case reads the
// ETag the mount answers for the body, and asks that it is the query the document wrote,
// that the body behind it is the sheet the chrome was handed, and that a `?v=` naming
// other bytes buys no year at all. A cache that keeps an address for a year is a cache
// that will not ask again: it has to be holding the sheet the page was written with, or
// everyone who walks the sequence after that is styled by rules no page asked for.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
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

// otherBytes is a `?v=` no sheet hashes to: the year spent on such an address would be a
// year spent on a query that names nothing.
const otherBytes = "0000000000000000"

// sheetTheWalkPaysFor is the stylesheet address a guided document writes, path and `?v=`
// kept apart: what a browser pays the year for is the whole address, query included.
var sheetTheWalkPaysFor = regexp.MustCompile(`<link rel="stylesheet" href="([^"?]+)\?v=([^"]*)"`)

// TestAGuidedPageBuysTheBytesItsAddressNames mounts the composed sheet on the App surface
// and writes each page of a guided walk against that surface's page address, then asks of
// the address each document tells the browser to fetch: that it answers with the sheet's
// own bytes and a stylesheet content type, that the ETag of those bytes is the `?v=` the
// document wrote, and that the same file at the same path with a query naming other bytes
// is revalidated rather than kept. A composition that fingerprints bytes other than the
// ones its sheet carries, a mount whose file is not the sheet the chrome was handed, and a
// static rule that gives the year to any query all refuse here, per page.
func TestAGuidedPageBuysTheBytesItsAddressNames(t *testing.T) {
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
		bought := sheetTheWalkPaysFor.FindStringSubmatch(writeDocument(t, page.Document(chrome, page.Request{Path: screen.mode}, view, g.Group(view.Body))))
		if bought == nil {
			t.Fatalf("the %s document writes no stylesheet address with a ?v=, so nothing here has bytes to be bought", screen.mode)
		}
		address, query := bought[1], bought[2]
		kept := buy(t, router, address+"?v="+query)
		if kept.Code != http.StatusOK {
			t.Fatalf("the %s document buys its sheet at %s and the mount answers %d", screen.mode, address, kept.Code)
		}
		if kind := kept.Header().Get("Content-Type"); !strings.HasPrefix(kind, "text/css") {
			t.Errorf("the %s document buys %s and the mount answers it as %q: a browser that does not read those bytes as a stylesheet drops every rule of the walk", screen.mode, address, kind)
		}
		if tag := strings.Trim(kept.Header().Get("ETag"), `"`); tag != query {
			t.Errorf("the %s document buys %s?v=%s, but the mount hashes the bytes behind that address as %q: the year is spent on a query the file does not carry, so a cache that keeps it holds CSS other than the sheet this page was written with",
				screen.mode, address, query, tag)
		}
		if body := kept.Body.String(); body != string(sheet.Body) {
			t.Errorf("the %s document buys %s and the mount answers %d bytes, not the %d of the sheet its chrome carries: the walk is styled by a file no page composed",
				screen.mode, address, len(body), len(sheet.Body))
		}
		if named := buy(t, router, address+"?v="+otherBytes); named.Code != http.StatusOK {
			t.Errorf("the sheet refuses a request whose ?v= names bytes it does not carry, answering %d; a query nobody holds hashes to has to reach the file and say so", named.Code)
		} else if got := named.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("the %s sheet at %s?v=%s says %q, want %q: the year belongs to the address naming these bytes and to no other query",
				screen.mode, address, otherBytes, got, "no-cache")
		}
	}
}
