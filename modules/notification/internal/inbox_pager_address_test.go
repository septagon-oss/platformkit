package internal_test

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

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

// inboxHost answers one tenant at one host and allows every grant: the question
// below is about the addresses a page links, which no guard decides.
type inboxHost struct{ tenant tenancy.Tenant }

func (s inboxHost) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if host != "acme.localhost" {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (inboxHost) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// TestTheInboxPagerLinksTheAddressThePageIsMountedAt: a person with more notices
// than one page holds reaches the older ones through the pager, so every page link
// the list draws is the list's own mounted address with a page number — the same
// address its live region re-reads — and never the bare collection path, which is
// not a page on this host.
func TestTheInboxPagerLinksTheAddressThePageIsMountedAt(t *testing.T) {
	_, conn := dbtest.Schema(t)
	me := notificationtest.Ada
	kernel, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: "localhost",
		Tenants:   inboxHost{tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}},
		Conn:      conn,
		Authorize: inboxHost{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: me}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	store := notificationtest.NewFake()
	for _, title := range []string{"first", "second", "third"} {
		if _, err := store.Notify(t.Context(), db.Tx[db.Tenant]{}, contracts.Notice{Recipient: me, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	surfaces := kernel.Surfaces("notification")
	internal.MountInApp(surfaces, internal.InApp{
		Shell: page.Shell{
			Chrome: page.Chrome{Brand: "PlatformKit", Assets: "/admin/assets",
				Stylesheet: ui.Compose(design.Default()), Scripts: []string{"htmx.min.js"}, SignIn: "/admin/login"},
			Back: "/admin", BackLabel: "Home",
			Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return h.Main(g.Group(body)) },
		},
		Read: store, PerPage: 1,
		Topics: []string{contracts.EventCreated, contracts.EventRead},
	})
	inbox := surfaces.App.PagePath("/notifications")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost"+inbox, nil)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "a-session-the-kernel-need-not-read"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d: %.400s", inbox, rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	// The region re-reads the mounted address; that is the page this person is on.
	if !strings.Contains(out, `hx-get="`+inbox+`"`) {
		t.Fatalf("the list was not drawn as a live region of %s:\n%.2000s", inbox, out)
	}
	if !strings.Contains(out, `data-page="2"`) {
		t.Fatalf("three notices at one a page drew no link to page 2:\n%.4000s", out)
	}
	if want := `href="` + inbox + `?page=2"`; !strings.Contains(out, want) {
		t.Errorf("the pager does not link %s, so the older notices are out of reach:\n%.4000s", want, pagerOf(out))
	}
}

// pagerOf is the part of the document that holds the page links, so a refusal
// prints what a person would click rather than the whole page.
func pagerOf(out string) string {
	if i := strings.Index(out, `data-page=`); i >= 0 {
		return out[max(i-300, 0):min(i+600, len(out))]
	}
	return out
}
