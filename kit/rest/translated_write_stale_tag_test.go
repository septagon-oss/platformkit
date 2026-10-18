package rest_test

// translated_write_stale_tag_test.go pins the row's own tag at the translated
// door. A resource with a translatable field mounts no plain read and no plain
// PATCH: the `?lang=` read is its only item read and the `?lang=` PATCH its only
// update, so if those two did not carry the tag the condition would be askable
// and unanswerable on every translatable resource. Three claims:
//
//   - the translated read answers the same ETag the plain read of a counted
//     entity would, whatever language it was asked in;
//   - a `?lang=` write quoting a tag the row has passed is refused with 412 and
//     reaches the port with nothing — a translation drafted against a source the
//     caller no longer holds is the stale write wearing another language;
//   - a `?lang=` write quoting the current tag is filed, and a tag quoted against
//     an entity that keeps no write count is 422 rather than a translation written
//     under a condition nothing could have checked.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// CountedArticle is the translated resource whose rows the kernel counts writes
// on: one translatable field and the revision the ETag is made of.
type CountedArticle struct {
	entity.Base
	Title    string `json:"title" i18n:"translatable"`
	Revision int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (CountedArticle) TableName() string { return "rest_counted_articles" }

const countedArticleDDL = `
CREATE TABLE rest_counted_articles (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	revision bigint NOT NULL DEFAULT 1
);
ALTER TABLE rest_counted_articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_counted_articles FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_counted_articles_tenant ON rest_counted_articles
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// mountedCountedArticle is the counted article behind the whole middleware chain,
// over the given port, with one row created through the resource's own door.
func mountedCountedArticle(t *testing.T, port rest.Translations) (chi.Router, string) {
	t.Helper()
	s := rest.Spec[*CountedArticle]{Name: rest.Name{One: "Counted", Other: "Counteds"},
		Module: "articles", Entity: "counted", Path: "/counted",
		Read: "article:read", Write: "article:write", Translations: port,
	}
	_, router, admin := mountAs(t, s, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), countedArticleDDL); err != nil {
		t.Fatalf("create rest_counted_articles: %v", err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/articles/counted", `{"title":"About us."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the counted article = %d %s, want 201", code, body)
	}
	return router, id(t, body)
}

// quoted is call with the one header these cases are about, and the response's
// headers kept: the tag travels in ETag and the condition in If-Match, and a case
// that can send neither cannot ask whether the door honoured them.
func quoted(t *testing.T, r http.Handler, method, path, body, ifMatch string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header()
}

// TestTheTranslatedReadCarriesTheRowsTag: the `?lang=` read of a counted entity
// answers the row's own write count beside the language it is written in.
func TestTheTranslatedReadCarriesTheRowsTag(t *testing.T) {
	router, articleID := mountedCountedArticle(t, &recordedPort{Translations: &stubPort{}})
	path := "/api/v1/articles/counted/" + articleID

	code, body, head := quoted(t, router, http.MethodGet, path+"?lang=pt-PT", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET ?lang=pt-PT = %d %s, want 200", code, body)
	}
	if tag := head.Get("ETag"); tag != `"1"` {
		t.Errorf("ETag on the translated read = %q, want the row's first revision, \"1\"", tag)
	}
	if lang := head.Get("Content-Language"); lang != "pt-PT" {
		t.Errorf("Content-Language = %q, want pt-PT beside the tag", lang)
	}
}

// TestAStaleTagOnALocaleWriteIsRefusedAndReachesThePortWithNothing: a translation
// quoted against a revision the row has passed is 412, the port is handed no save,
// and the same body quoting the current tag is filed under its language.
func TestAStaleTagOnALocaleWriteIsRefusedAndReachesThePortWithNothing(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedCountedArticle(t, port)
	path := "/api/v1/articles/counted/" + articleID

	_, body, head := quoted(t, router, http.MethodGet, path+"?lang=en", "", "")
	stale := head.Get("ETag")
	if stale != `"1"` {
		t.Fatalf("the row's first tag = %q in %s, want \"1\"", stale, body)
	}
	code, body, head := quoted(t, router, http.MethodPatch, path, `{"title":"About us, since 1990."}`, "")
	if code != http.StatusOK || head.Get("ETag") != `"2"` {
		t.Fatalf("the source write = %d %s with ETag %q, want 200 and \"2\"", code, body, head.Get("ETag"))
	}

	code, body, _ = quoted(t, router, http.MethodPatch, path+"?lang=pt-PT", `{"title":"Sobre nós."}`, stale)
	if code != http.StatusPreconditionFailed {
		t.Errorf("PATCH ?lang=pt-PT quoting the tag read before the source moved = %d %s, want 412", code, body)
	}
	if len(port.saved) != 0 {
		t.Errorf("a refused translation reached the port: %+v", port.saved)
	}

	code, body, head = quoted(t, router, http.MethodPatch, path+"?lang=pt-PT", `{"title":"Sobre nós."}`, `"2"`)
	if code != http.StatusOK {
		t.Fatalf("PATCH ?lang=pt-PT quoting the current tag = %d %s, want 200", code, body)
	}
	if len(port.saved) != 1 || port.saved[0].Locale != "pt-PT" || port.saved[0].Values["title"] != "Sobre nós." {
		t.Errorf("the translation quoting the current tag was not filed under pt-PT: %+v", port.saved)
	}
	if tag := head.Get("ETag"); tag != `"2"` {
		t.Errorf("ETag after a translation write = %q, want \"2\": a translation does not move the record's own count", tag)
	}
	if _, body, _ := quoted(t, router, http.MethodGet, path+"?lang=en", "", ""); !strings.Contains(body, `"title":"About us, since 1990."`) {
		t.Errorf("the source moved under a translation write: %s", body)
	}
}

// TestATagQuotedAgainstAnUncountedTranslatedRowIsRefused: the plain article keeps
// no write count, so its translated read sends no tag and a `?lang=` write that
// quotes one is 422 with nothing filed, as the plain PATCH of such an entity is.
func TestATagQuotedAgainstAnUncountedTranslatedRowIsRefused(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)
	path := "/api/v1/articles/article/" + articleID

	code, body, head := quoted(t, router, http.MethodGet, path+"?lang=pt-PT", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET ?lang=pt-PT = %d %s, want 200", code, body)
	}
	if tag := head.Get("ETag"); tag != "" {
		t.Errorf("ETag on an entity with no write count = %q, want none", tag)
	}
	code, body, _ = quoted(t, router, http.MethodPatch, path+"?lang=pt-PT", `{"title":"Sobre nós."}`, `"1"`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("PATCH ?lang=pt-PT quoting a tag against an uncounted row = %d %s, want 422", code, body)
	}
	if len(port.saved) != 0 {
		t.Errorf("a translation under a condition nothing could check reached the port: %+v", port.saved)
	}
}
