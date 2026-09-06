package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/web"
)

const host = "acme.test"

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

type tenants struct{}

func (tenants) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h == host {
		return acme, nil
	}
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

func (tenants) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// site mounts the module over the two fakes on a real connection, as the
// application would, and returns the fakes to publish into.
func site(t *testing.T) (http.Handler, *sitetest.Fake, *contenttest.Fake) {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: tenants{}, Conn: app, Authorize: tenants{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	settings, contents := sitetest.NewFake(), contenttest.NewFake()
	m := web.Module(web.Deps{Site: settings, Content: contents, Theme: design.Default()})
	if err := module.Validate([]module.Module{m}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	m.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("declarations: %v", err)
	}
	return router, settings, contents
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

func TestAFreshSiteSaysSoAndPointsAtTheAdmin(t *testing.T) {
	h, _, _ := site(t)
	status, body := get(t, h, "/")
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	for _, want := range []string{"Nothing published yet", `href="/admin/login"`, `<title>Welcome · Acme</title>`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// No controllers: the one script on an unpinned page is the inline snippet
	// that applies a theme the visitor stored, which the shell shares.
	if strings.Contains(body, "<script src=") {
		t.Error("the site loads a controller")
	}
}

func TestTheHomeIsThePublishedPageTheSettingsName(t *testing.T) {
	h, settings, contents := site(t)
	ctx := t.Context()
	if _, err := settings.Save(ctx, db.Tx[db.Tenant]{}, &sitecontracts.SiteSettings{
		Title: "Acme Journal", Tagline: "Notes from the workshop", HomeSlug: "welcome",
		Theme: "dark", PrimaryColor: "#c0ffee",
		Nav: sitecontracts.Nav{{Label: "About", Path: "/about"}},
	}); err != nil {
		t.Fatal(err)
	}
	id := contents.Put(&contentcontracts.Content{Slug: "welcome", Title: "Welcome home", Kind: "page",
		Body: "# Hello\n\nThis is **home**, with a [link](/about) and `code`.\n\n<script>alert(1)</script>"})
	if _, err := contents.Publish(ctx, db.Tx[db.Tenant]{}, id); err != nil {
		t.Fatal(err)
	}
	status, body := get(t, h, "/")
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	for _, want := range []string{
		`data-theme="dark"`, `--pk-color-accent-default:#c0ffee`, "Acme Journal", "Notes from the workshop",
		`href="/about"`, "Welcome home", "<strong>home</strong>", "<code>code</code>", `<title>Welcome home · Acme</title>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "platformkit-theme") {
		t.Error("a pinned document carried a script or the visitor's theme")
	}
	if status, page := get(t, h, "/welcome"); status != http.StatusOK || !strings.Contains(page, "Welcome home") {
		t.Fatalf("the page by slug: %d", status)
	}
}

func TestAMissingHomePageAndAnUnknownSlugAreSaidPlainly(t *testing.T) {
	h, settings, _ := site(t)
	if _, err := settings.Save(t.Context(), db.Tx[db.Tenant]{}, &sitecontracts.SiteSettings{HomeSlug: "later", Theme: "system", PrimaryColor: "#2563eb"}); err != nil {
		t.Fatal(err)
	}
	if status, body := get(t, h, "/"); status != http.StatusOK || !strings.Contains(body, "The home page is not published") {
		t.Fatalf("unpublished home: %d %s", status, body)
	}
	for _, path := range []string{"/nope", "/favicon.ico", "/Not-A-Slug"} {
		status, body := get(t, h, path)
		if status != http.StatusNotFound || !strings.Contains(body, "Back to the site") {
			t.Errorf("%s: %d, want a 404 page with a way back", path, status)
		}
	}
}

func TestAnUnknownHostServesNoSite(t *testing.T) {
	h, _, _ := site(t)
	req := httptest.NewRequest(http.MethodGet, "http://nobody.test/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
