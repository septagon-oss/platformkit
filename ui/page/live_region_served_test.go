package page_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// A live region re-reads its own address when the server says its state changed, and
// the answer to that read is the region's contents and nothing else: htmx puts them
// into the element the region already is. A whole document there would be a page
// inside a page, and a repeat of the owner would be a second connection replacing the
// one the refresh arrived on.
//
// The header decides the shape of an answer and none of its content. Both shapes come
// from one handler behind one guard, so what a caller may see cannot depend on a
// header they might have typed: the rows are read the same way whichever shape the
// response takes.

// liveCaller is the person every request below arrives as, and liveTenant answers for
// the one tenant on one host: the question these cases ask is about a response's
// shape, which the guard does not decide.
var liveCaller = tenancy.Principal{UserID: uuid.MustParse("6f6b9b3f-4c6f-4b1e-9b0e-2f3b9a1c7d21")}

type liveTenant struct{ tenant tenancy.Tenant }

func (s liveTenant) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if host != "acme.localhost" {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (liveTenant) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// livePages mounts two workspace pages over a kernel of their own — one that names the
// events its body is a region for, one that names none — and answers the address each
// is mounted at.
func livePages(t *testing.T) (router http.Handler, inbox, plain string) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	kernel, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: "localhost",
		Tenants:   liveTenant{tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}},
		Conn:      conn,
		Authorize: liveTenant{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return liveCaller, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shell := liveShell()
	surfaces := kernel.Surfaces("live")
	page.Serve(surfaces.App, shell, page.Route{ID: "live-inbox", Method: http.MethodGet,
		Path: "/inbox", Summary: "My notices"}, httpx.SignedIn(),
		func(_ context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return page.View{Title: "Inbox", Live: []string{"notification.created", "notification.read"},
				Body: []g.Node{h.P(g.Text("one notice"))}}, nil
		})
	page.Serve(surfaces.App, shell, page.Route{ID: "live-plain", Method: http.MethodGet,
		Path: "/plain", Summary: "An ordinary page"}, httpx.SignedIn(),
		func(_ context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return page.View{Title: "Plain", Body: []g.Node{h.P(g.Text("an ordinary page"))}}, nil
		})
	return router, surfaces.App.PagePath("/inbox"), surfaces.App.PagePath("/plain")
}

func liveShell() page.Shell {
	return page.Shell{Chrome: chrome(), Back: "/admin", BackLabel: "Home",
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
			return h.Main(h.H2(g.Text("Acme")), g.Group(body))
		}}
}

// openWorkspace asks for one address as somebody who is signed in — the kernel reads
// a credential before it will ask who is calling, so a request with no cookie is
// anonymous whatever the authenticator would have said — and returns what came back.
func openWorkspace(t *testing.T, router http.Handler, url, htmx string) string {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost"+url, nil)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "a-session-the-kernel-need-not-read"})
	if htmx != "" {
		req.Header.Set("HX-Request", htmx)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d: %s", url, rec.Code, firstLine(rec.Body.String()))
	}
	return rec.Body.String()
}

// TestALivePageIsDrawnAsAConnectionAndARegion is the first paint: the document carries
// the owner that holds one connection, the region a refresh re-reads, and the address
// of the kernel's own live door — named once by the kernel rather than spelled out by
// whoever drew the page.
func TestALivePageIsDrawnAsAConnectionAndARegion(t *testing.T) {
	router, inbox, _ := livePages(t)
	out := openWorkspace(t, router, inbox, "")
	for _, want := range []string{
		"<!doctype html>",
		`hx-ext="sse"`,
		`sse-connect="` + httpx.StreamAddress + `?topics=notification.created%2Cnotification.read"`,
		`hx-get="` + inbox + `"`,
		`sse-swap="notification.created,notification.read,resync"`,
		`data-controller="live"`,
		"<main><h2>Acme</h2>",
		`/js/htmx-ext-sse-2.2.4.min.js`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the document a person opens lacks %s:\n%s", want, out)
		}
	}
	if strings.Index(out, `data-controller="live"`) < strings.Index(out, `hx-ext="sse"`) {
		t.Errorf("the region is drawn outside the element holding its connection:\n%s", out)
	}
}

// TestARegionIsAnsweredWithItsOwnContents is the refresh: neither a document nor a
// second owner, and the region's contents in full.
func TestARegionIsAnsweredWithItsOwnContents(t *testing.T) {
	router, inbox, _ := livePages(t)
	out := openWorkspace(t, router, inbox, "true")
	for _, unwanted := range []string{"<!doctype", "<html", "<head", "<main", "hx-ext", "sse-connect", "sse-swap", "<script"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the swap answer carries %s, which would nest a page inside a page or replace the connection that asked:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "<p>one notice</p>") {
		t.Errorf("the swap answer does not carry the region's contents:\n%s", out)
	}
}

// TestAnOrdinaryPageAnswersHtmxAsADocument is the other half of the opt-in: a page that
// named no topic keeps the answer it has always given, because nothing said it had a
// region to swap.
func TestAnOrdinaryPageAnswersHtmxAsADocument(t *testing.T) {
	router, _, plain := livePages(t)
	out := openWorkspace(t, router, plain, "true")
	for _, want := range []string{"<!doctype html>", "<main><h2>Acme</h2>", "<p>an ordinary page</p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("an ordinary page lost its document for an HX-Request (%s):\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"hx-ext", "sse-connect", "sse-swap", "htmx-ext-sse"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a page with no live region carries %s:\n%s", unwanted, out)
		}
	}
}

// TestARefreshKeepsThePageItWasDrawnAt: a person on the second page of a list is not
// returned to the first by somebody else's notice. The address is the caller's own
// bytes, which is why the composed value is held to the kernel's local-path rule
// before it is ever written into an attribute.
func TestARefreshKeepsThePageItWasDrawnAt(t *testing.T) {
	router, inbox, _ := livePages(t)
	out := openWorkspace(t, router, inbox+"?page=2", "")
	if !strings.Contains(out, `hx-get="`+inbox+`?page=2"`) {
		t.Errorf("the region was drawn to re-read the first page rather than the one it came from:\n%s", out)
	}
}
