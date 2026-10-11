package site_test

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

const (
	host     = "acme.test"
	settings = "/api/v1/site/settings"
	// The public face is a public door, and a public door answers under the
	// public prefix: /api/v1/public/<module>/<resource>. The old address, under
	// the module's workspace prefix, redirects here for one release — see
	// kit/httpx/aliases.go and TestTheAliasRedirectsAndNeverServes.
	public = "/api/v1/public/site/settings"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

func mounted(t *testing.T) chi.Router {
	t.Helper()
	_, conn := dbtest.Schema(t, site.Migrations)
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, sites := site.New(site.Deps{})
	sites.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
}

func send(t *testing.T, r http.Handler, method, at, body string, signedIn bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(body))
	if signedIn {
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestTheSiteIsReadAndPutAndNothingElse. A singleton has no collection: there
// is no list, no create and no delete, and a tenant that has configured nothing
// reads the defaults rather than a 404.
func TestTheSiteIsReadAndPutAndNothingElse(t *testing.T) {
	router := mounted(t)

	code, body := send(t, router, http.MethodGet, settings, "", true)
	if code != http.StatusOK || !strings.Contains(body, `"theme":"system"`) {
		t.Fatalf("GET %s before anything = %d %s, want the defaults", settings, code, body)
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		if code, _ := send(t, router, method, settings, `{}`, true); code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405: the settings of a tenant are not created and not removed", method, settings, code)
		}
	}

	code, body = send(t, router, http.MethodPut, settings,
		`{"title":"Acme","tagline":"We make things","theme":"dark","primaryColor":"#B45309","nav":[{"label":"About","path":"/about-us"}]}`, true)
	if code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s, want 200", settings, code, body)
	}
	if !strings.Contains(body, `"primaryColor":"#b45309"`) {
		t.Errorf("the colour was stored as %s; it is lower-cased so a theme has one spelling to read", body)
	}
	if code, body = send(t, router, http.MethodPut, settings, `{"title":"Acme","primaryColor":"nope"}`, true); code != http.StatusUnprocessableEntity {
		t.Errorf("PUT with a colour that is not one = %d %s, want 422", code, body)
	}
	// The syntax and the reading are two refusals, and the second names the two
	// numbers and the two canvases, because the person on the other end of it has
	// to be able to pick another colour without a colour chart.
	if code, body = send(t, router, http.MethodPut, settings, `{"title":"Acme","primaryColor":"#ff8800"}`, true); code != http.StatusUnprocessableEntity {
		t.Errorf("PUT with an accent that vanishes into the light canvas = %d %s, want 422", code, body)
	}
	for _, want := range []string{"2.08", "7.67", "#f2efe7", "#0e1614"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal of #ff8800 does not say %s: %s", want, body)
		}
	}
}

// TestTheCanvasesAreTheKitsOwn is what makes the two hex values in
// contracts/contrast.go honest rather than a second palette registry: they are
// design's own surfaces, written out because a module's contracts/ names no theme
// and no stylesheet, and checked here, in the one package of this module allowed
// to import design. A theme that moves turns this red naming both spellings.
func TestTheCanvasesAreTheKitsOwn(t *testing.T) {
	t.Parallel()
	if got, want := contracts.CanvasLight, design.Light().SurfaceCanvas; got != want {
		t.Errorf("contracts.CanvasLight is %s and design.Light().SurfaceCanvas is %s: one canvas, two spellings, and the accent rule is measuring the wrong one", got, want)
	}
	if got, want := contracts.CanvasDark, design.Dark().SurfaceCanvas; got != want {
		t.Errorf("contracts.CanvasDark is %s and design.Dark().SurfaceCanvas is %s: one canvas, two spellings, and the accent rule is measuring the wrong one", got, want)
	}
}

// TestTheAccentRatiosAreTheOnesTheRuleCompared is the measured table, so that a
// change to the arithmetic fails here with numbers beside it rather than in
// whoever's browser. The two numbers are the ratios against the light canvas and
// the dark one; whether the pair clears MinAccentRatio is the case in
// sitetest/conformance.go, which is run against the database as well as the fake.
func TestTheAccentRatiosAreTheOnesTheRuleCompared(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		colour      string
		light, dark float64
	}{
		{"#2563eb", 4.50, 3.55}, // the kit's own default
		{"#b45309", 4.37, 3.66},
		{"#0f766e", 4.76, 3.36},
		{"#7c3aed", 4.96, 3.22},
		{"#7f7f7f", 3.48, 4.59},
		{"#8a8a8a", 3.00, 5.32}, // 3.0044 rounded — the nearest colour above the band's edge
		{"#8b8b8b", 2.97, 5.39}, // one step lighter and under it
		{"#1d4ed8", 5.83, 2.74}, // reads on paper, not in the dark
		{"#ff8800", 2.08, 7.67}, // and the other way round
		{"#c0ffee", 1.03, 16.42},
		{"#ffffff", 1.15, 18.36},
		{"#0e1614", 15.98, 1.00},
	} {
		light, dark, ok := contracts.AccentRatios(tc.colour)
		switch {
		case !ok:
			t.Errorf("AccentRatios(%q): not a colour", tc.colour)
		case light != tc.light || dark != tc.dark:
			t.Errorf("%q reads %.2f / %.2f, want %.2f / %.2f", tc.colour, light, dark, tc.light, tc.dark)
		}
		// The ratio is symmetric, which is the only check that the arithmetic is
		// the standard's and not a formula that happens to agree in one direction.
		if r, err := contracts.ContrastRatio(tc.colour, contracts.CanvasLight); err != nil || round2(r) != tc.light {
			t.Errorf("ContrastRatio(%q, light) = %v, %v", tc.colour, r, err)
		}
		if r, err := contracts.ContrastRatio(contracts.CanvasLight, tc.colour); err != nil || round2(r) != tc.light {
			t.Errorf("ContrastRatio(light, %q) = %v, %v: a ratio is the same either way round", tc.colour, r, err)
		}
	}
	if _, err := contracts.ContrastRatio("#2563eb", "canvas"); err == nil {
		t.Error("ContrastRatio answered for a canvas that is not a colour")
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// TestThePublicRouteCarriesWhatAThemeNeedsAndNothingElse: a visitor with no
// session reads the name, the navigation and the colour scheme. The home slug
// and the logo are internal references and the timestamps are nobody's
// business, so a public response that carried the whole row would be an admin
// screen anybody could read.
func TestThePublicRouteCarriesWhatAThemeNeedsAndNothingElse(t *testing.T) {
	router := mounted(t)

	// An unconfigured site still answers, with an empty navigation rather than
	// a null: a theme should not have to guard against one.
	code, body := send(t, router, http.MethodGet, public, "", false)
	if code != http.StatusOK || !strings.Contains(body, `"nav":[]`) {
		t.Fatalf("GET %s before anything = %d %s", public, code, body)
	}

	if code, body = send(t, router, http.MethodPut, settings,
		`{"title":"Acme","tagline":"secret","homeSlug":"welcome","theme":"dark","nav":[{"label":"About","path":"/about-us"}]}`, true); code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", settings, code, body)
	}

	code, body = send(t, router, http.MethodGet, public, "", false)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want 200", public, code, body)
	}
	for _, want := range []string{`"title":"Acme"`, `"theme":"dark"`, `"label":"About"`, `"path":"/about-us"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the public site does not carry %s:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"tagline", "secret", "homeSlug", "createdAt", "primaryColor"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the public site carries %q, which a visitor has no business with:\n%s", forbidden, body)
		}
	}
	// And the settings themselves are not public.
	if code, _ = send(t, router, http.MethodGet, settings, "", false); code != http.StatusForbidden {
		t.Errorf("an anonymous read of the settings = %d, want 403", code)
	}
}

// TestANavigationCannotLeaveTheSite is the review's finding: the documentation
// said a navigation refuses absolute URLs and the check was a leading slash, so
// "//evil.example" — which every browser resolves as another origin — was
// accepted and rendered as a link in the tenant's own menu.
//
// The rule itself is now httpx.LocalPath's, because the admin sign-in form had
// a second copy of it and that copy was the wrong one. This is the half that
// matters here: the entity refuses what the kernel refuses.
func TestANavigationCannotLeaveTheSite(t *testing.T) {
	for _, tt := range []struct {
		path string
		want bool
	}{
		{"/about-us", true},
		{"//evil.example", false},       // a network-path reference
		{`/\evil.example`, false},       // the same, spelled with the character a browser normalises
		{"https://evil.example", false}, // the one the old check did catch
		{"about-us", false},             // relative to whatever page it is on
		{"/", true},                     // the home page is a path
		{"/search?q=a#top", true},       // a query and a fragment are part of a path
		{`/a\b`, false},                 // a backslash has no meaning in a path
		{"javascript:alert(1)", false},  // not a path at all
		{"////evil.example", false},     // more slashes are still an authority
	} {
		if got := httpx.LocalPath(tt.path); got != tt.want {
			t.Errorf("httpx.LocalPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
		// And through the entity's own Validate, which is the door a request
		// actually comes through.
		s := contracts.SiteSettings{Nav: contracts.Nav{{Label: "Link", Path: tt.path}}}
		if err := s.Validate(t.Context()); (err == nil) != tt.want {
			t.Errorf("Validate with nav %q = %v, want accepted=%v", tt.path, err, tt.want)
		}
	}
}

// TestALabelIsCountedInCharacters: len() counts bytes, so a menu in any
// language but English was refused three times too early.
func TestALabelIsCountedInCharacters(t *testing.T) {
	s := contracts.SiteSettings{
		Title: strings.Repeat("日", contracts.MaxTitle),
		Nav:   contracts.Nav{{Label: strings.Repeat("é", contracts.MaxLabel), Path: "/x"}},
	}
	if err := s.Validate(t.Context()); err != nil {
		t.Errorf("a title and a label of exactly their limits in characters: %v", err)
	}
	s.Title += "日"
	if err := s.Validate(t.Context()); err == nil {
		t.Error("a title one character past the limit was accepted")
	}
}

// TestAHomeSlugIsBoundedByTheColumnThatHoldsIt. MaxHomeSlug was declared beside
// the other four bounds and never read, so a slug longer than the varchar(200)
// that stores it reached the database and came back as a 500 rather than as a
// 422 saying what was wrong.
func TestAHomeSlugIsBoundedByTheColumnThatHoldsIt(t *testing.T) {
	fits := contracts.SiteSettings{Title: "Acme", HomeSlug: strings.Repeat("a", contracts.MaxHomeSlug)}
	if err := fits.Validate(t.Context()); err != nil {
		t.Errorf("a slug of exactly %d characters = %v, want it accepted", contracts.MaxHomeSlug, err)
	}
	over := contracts.SiteSettings{Title: "Acme", HomeSlug: strings.Repeat("a", contracts.MaxHomeSlug+1)}
	err := over.Validate(t.Context())
	if err == nil {
		t.Fatalf("a slug of %d characters was accepted into a varchar(%d)", contracts.MaxHomeSlug+1, contracts.MaxHomeSlug)
	}
	if !strings.Contains(err.Error(), "home slug") {
		t.Errorf("the refusal is %q, which does not say which field is too long", err)
	}
}

// surfacesOf is the module's view of the kernel: the three routers, named the
// way a composition names them at mount. The test keeps the *httpx.API
// separately, because validating the composition is the composition's job and
// holding a *Router would be holding one door of three.
func surfacesOf(a *httpx.API) httpx.Surfaces { return a.Surfaces("site") }
