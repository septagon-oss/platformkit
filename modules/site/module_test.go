package site_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	// The public face moved with the port onto rest.Singleton: a singleton
	// declares one path and its public door is under it. Nothing has been
	// released, so this is a rename and not a break.
	public = "/api/v1/site/settings/public"
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
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	site.Module(site.Deps{}).Routes(api)
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
		`{"title":"Acme","tagline":"We make things","theme":"dark","primaryColor":"#FF8800","nav":[{"label":"About","path":"/about-us"}]}`, true)
	if code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s, want 200", settings, code, body)
	}
	if !strings.Contains(body, `"primaryColor":"#ff8800"`) {
		t.Errorf("the colour was stored as %s; it is lower-cased so a theme has one spelling to read", body)
	}
	if code, body = send(t, router, http.MethodPut, settings, `{"title":"Acme","primaryColor":"nope"}`, true); code != http.StatusUnprocessableEntity {
		t.Errorf("PUT with a colour that is not one = %d %s, want 422", code, body)
	}
}

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
