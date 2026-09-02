package content_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

const (
	host   = "acme.test"
	path   = "/api/v1/content/contents"
	public = "/api/v1/content/public/"
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

// mounted is the module as main mounts it: the manifest's own Routes, on the
// real API, against a real Postgres.
func mounted(t *testing.T) (*httpx.API, chi.Router) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	content.Module(content.Deps{}).Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router
}

// call is a signed-in caller; anonymous is the reader the public route is for,
// and the difference is only the cookie: see kit/httpx.credentialed.
func call(t *testing.T, r http.Handler, method, at, body string) (int, string) {
	t.Helper()
	return send(t, r, method, at, body, true)
}

func anonymous(t *testing.T, r http.Handler, at string) (int, string) {
	t.Helper()
	return send(t, r, http.MethodGet, at, "", false)
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

// TestThePatchRefusesTheFieldsARouteOwns. Three of a page's fields belong to a
// route: status and publishedAt to the three commands, which move them together
// and announce it, and author to the create that stamped it from the caller. A
// PATCH that could set status would serve a page with no publication time and
// tell nobody; one that could set author would forge a byline.
func TestThePatchRefusesTheFieldsARouteOwns(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path, `{"slug":"about-us","title":"About us","body":"hello"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := path + "/" + field(t, body, "id")

	for _, tt := range []struct{ field, patch string }{
		{"status", `{"status":"published"}`},
		{"publishedAt", `{"publishedAt":"2020-01-01T00:00:00Z"}`},
		{"author", `{"author":"` + uuid.NewString() + `"}`},
	} {
		code, body := call(t, router, http.MethodPatch, at, tt.patch)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH %s = %d %s, want 422", tt.patch, code, body)
		}
		if !strings.Contains(body, tt.field+" belongs to a route of its own") {
			t.Errorf("PATCH %s answered %s, which does not name the field it refused", tt.patch, body)
		}
	}
	if code, body := call(t, router, http.MethodPatch, at, `{"title":"About Acme"}`); code != http.StatusOK ||
		!strings.Contains(body, "About Acme") {
		t.Errorf("PATCH of the title = %d %s, want 200: no route owns it", code, body)
	}
}

// TestThePublicRouteServesPublishedContentToAnybody is the whole point of
// publishing: a reader with no session reads the rendered page, and only when
// somebody published it. The HTML is what the sanitizer left, so a body with a
// script in it is a page with no script in it.
func TestThePublicRouteServesPublishedContentToAnybody(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path,
		`{"slug":"About Us","title":"About us","kind":"page","body":"# Hello\n\n<script>alert(1)</script>\n\nSome **bold** words."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	id := field(t, body, "id")

	// A draft is not served, and it is a 404 rather than a 403: from outside,
	// content that is not served and content that does not exist are one fact.
	if code, body = anonymous(t, router, public+"about-us"); code != http.StatusNotFound {
		t.Errorf("an unpublished page = %d %s, want 404", code, body)
	}
	if code, body = call(t, router, http.MethodPost, path+"/"+id+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish = %d %s, want 200", code, body)
	}

	code, body = anonymous(t, router, public+"about-us")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want 200", public+"about-us", code, body)
	}
	for _, want := range []string{`"slug":"about-us"`, `"kind":"page"`, "<h1", "<strong>bold</strong>", `"publishedAt"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the public page does not carry %s:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"<script", "alert(1)", `"body"`, `"author"`, `"status"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the public page carries %q, which a reader has no business with:\n%s", forbidden, body)
		}
	}

	// Archived is served to nobody either, which is the same 404.
	if code, body = call(t, router, http.MethodPost, path+"/"+id+"/archive", ""); code != http.StatusOK {
		t.Fatalf("archive = %d %s, want 200", code, body)
	}
	if code, body = anonymous(t, router, public+"about-us"); code != http.StatusNotFound {
		t.Errorf("an archived page = %d %s, want 404", code, body)
	}
	// A slug that is not one is refused at the door rather than queried for.
	if code, _ = anonymous(t, router, public+"Not%20A%20Slug"); code != http.StatusUnprocessableEntity {
		t.Errorf("a slug that could never have been stored = %d, want 422", code)
	}
}

// TestEveryEventTheRoutesDeclareIsInTheManifest is the boot gate kit/app runs,
// against this module alone.
func TestEveryEventTheRoutesDeclareIsInTheManifest(t *testing.T) {
	api, _ := mounted(t)
	declared := api.Events()
	for _, want := range []string{contracts.EventPublished, contracts.EventUnpublished, contracts.EventArchived} {
		if !slices.Contains(declared, want) {
			t.Errorf("the routes declare %v; %s is not among them", declared, want)
		}
	}
	for _, e := range declared {
		if !slices.Contains(contracts.Events, e) {
			t.Errorf("a route publishes %q and the manifest does not name it", e)
		}
	}
}

func field(t *testing.T, body, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `"`+name+`":"`)
	if !ok {
		t.Fatalf("no %s in %s", name, body)
	}
	out, _, _ := strings.Cut(rest, `"`)
	return out
}
