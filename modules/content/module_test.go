package content_test

import (
	"context"
	"encoding/json"
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
	_, contents := content.Module(content.Deps{})
	contents.Routes(api)
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

// TestThePublicPageIsConditional. Rendering is the expensive half of the public
// route — the review measured 2.25 seconds for one large body, on every
// anonymous request — so a reader who already has the page is told so and the
// renderer never runs.
func TestThePublicPageIsConditional(t *testing.T) {
	_, router := mounted(t)
	code, out := call(t, router, http.MethodPost, path, `{"slug":"about-us","title":"About us","body":"# Hello"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	id := field(t, out, "id")
	if code, out = call(t, router, http.MethodPost, path+"/"+id+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish = %d %s", code, out)
	}

	first := get(t, router, public+"about-us", "")
	tag := first.Header().Get("ETag")
	switch {
	case first.Code != http.StatusOK:
		t.Fatalf("the public page = %d %s", first.Code, first.Body.String())
	case tag == "":
		t.Fatal("the public page carries no ETag, so every reader renders it again")
	case !strings.Contains(first.Body.String(), "<h1"):
		t.Errorf("the page did not render: %s", first.Body.String())
	}

	again := get(t, router, public+"about-us", tag)
	if again.Code != http.StatusNotModified {
		t.Errorf("the same page with its own tag = %d, want 304", again.Code)
	}
	if again.Header().Get("ETag") != tag {
		t.Errorf("the 304 carries %q, want the tag it was asked about", again.Header().Get("ETag"))
	}
	// A tag from another version, and the wildcard, are the two other things a
	// caller sends.
	if stale := get(t, router, public+"about-us", `W/"nonsense"`); stale.Code != http.StatusOK {
		t.Errorf("a stale tag = %d, want the page", stale.Code)
	}

	// And an edit moves the tag, or a reader keeps a page that has changed.
	if code, out = call(t, router, http.MethodPatch, path+"/"+id, `{"body":"# Hello again"}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", code, out)
	}
	if edited := get(t, router, public+"about-us", tag); edited.Code != http.StatusOK {
		t.Errorf("after an edit the old tag = %d, want the new page", edited.Code)
	}
}

// get is one anonymous public read, with an optional If-None-Match.
func get(t *testing.T, r http.Handler, at, tag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+at, nil)
	if tag != "" {
		req.Header.Set("If-None-Match", tag)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestABodyHasACeiling: the public route renders this column on every anonymous
// request, so an unbounded body is a way to take the site down with one write.
func TestABodyHasACeiling(t *testing.T) {
	_, router := mounted(t)
	body := `{"slug":"big","title":"Big","body":"` + strings.Repeat("x", contracts.MaxBody+1) + `"}`
	if code, out := call(t, router, http.MethodPost, path, body); code != http.StatusUnprocessableEntity {
		t.Errorf("a body past the ceiling = %d %s, want 422", code, out[:min(len(out), 200)])
	}
	// Exactly the ceiling is stored, which is what makes it a boundary.
	body = `{"slug":"just","title":"Just","body":"` + strings.Repeat("x", contracts.MaxBody) + `"}`
	if code, out := call(t, router, http.MethodPost, path, body); code != http.StatusCreated {
		t.Errorf("a body of exactly the ceiling = %d %s", code, out[:min(len(out), 200)])
	}
	// The route above is refused by the schema, which is the first of three
	// checks; this is the second, and it is the one a caller that did not come
	// through huma meets — a hook, a seed, another module's write. The third is
	// the column's own CHECK.
	over := contracts.Content{Slug: "big", Title: "Big", Body: strings.Repeat("x", contracts.MaxBody+1)}
	if err := over.Validate(t.Context()); err == nil {
		t.Error("the entity accepted a body past the ceiling")
	}
	// And a title is counted in characters, not bytes: 200 Chinese characters
	// are 600 bytes and used to be refused.
	long := contracts.Content{Slug: "long", Title: strings.Repeat("日", contracts.MaxTitle)}
	if err := long.Validate(t.Context()); err != nil {
		t.Errorf("a title of exactly the limit in characters: %v", err)
	}
	long.Title += "日"
	if err := long.Validate(t.Context()); err == nil {
		t.Error("a title one character past the limit was accepted")
	}
}

// TestASlugSurvivesItsLanguage. Slugify used to drop every character outside
// a-z0-9, so "Über uns" became "ber-uns" and "Ærø" became nothing at all — a
// title an author could type and a 422 they could not act on.
func TestASlugSurvivesItsLanguage(t *testing.T) {
	for _, tt := range []struct{ title, want string }{
		{"Über uns", "uber-uns"},
		{"Ærø i Danmark", "aero-i-danmark"},
		{"Grüße aus Köln", "grusse-aus-koln"},
		{"Câțiva ani", "cativa-ani"},
		{"Zażółć gęślą jaźń", "zazolc-gesla-jazn"},
		{"Crème brûlée", "creme-brulee"},
		{"About Us", "about-us"},
		{"  spaced  out  ", "spaced-out"},
	} {
		if got := contracts.Slugify(tt.title); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

// TestAMarkdownLinkSaysWhereItGoes. "[click](//evil.example)" reads like a path
// within this site, in the source and in the rendered anchor, and every browser
// resolves it against the current scheme and leaves the site.
func TestAMarkdownLinkSaysWhereItGoes(t *testing.T) {
	_, router := mounted(t)
	const body = `[a](//evil.example) [b](/\evil.example) [c](/about-us) [d](https://example.com) [e](#top)`
	code, out := call(t, router, http.MethodPost, path,
		`{"slug":"links","title":"Links","body":`+quote(body)+`}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	id := field(t, out, "id")
	if code, out = call(t, router, http.MethodPost, path+"/"+id+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish = %d %s", code, out)
	}
	html := get(t, router, public+"links", "").Body.String()
	for _, want := range []string{
		`https://evil.example`, // rewritten, and now visibly somebody else's
		`/about-us`,            // an ordinary path is untouched
		`https://example.com`,  // so is an ordinary absolute URL
		`#top`,                 // and an anchor
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the rendered page does not carry %s: %s", want, html)
		}
	}
	// The two forms of the trick are gone: no link is left that a browser would
	// read as an authority.
	for _, gone := range []string{`href=\"//`, `href=\"/\\`} {
		if strings.Contains(html, gone) {
			t.Errorf("a protocol-relative link survived: %s", html)
		}
	}
}

// quote is one string as a JSON one.
func quote(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(out)
}
