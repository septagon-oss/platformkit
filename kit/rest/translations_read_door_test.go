package rest_test

// translations_read_door_test.go pins the read side of the translation port: the
// two doors of a resource that declares a translatable field answer `?lang=`, the
// values in the response are that language's, every field that could not be
// answered is named in `_i18n`, and the response says which language it is in.
//
// The arms are the three ways this feature can lie. Serving a translation while
// saying nothing about the field that is not one is the first. Answering a
// language the tenant never declared is the second. And advertising `?lang=` on
// every resource in the installation, translated or not, is the third — so the
// plain Task door is asked here, in the same file, to prove its bytes did not
// move and that its own document never offered the parameter.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Article is the translated resource: two translatable fields, one of which the
// stub port holds in Portuguese and one of which it does not.
type Article struct {
	entity.Base
	Title string `json:"title" i18n:"translatable"`
	Body  string `json:"body,omitempty" gorm:"type:text" i18n:"translatable"`
}

func (Article) TableName() string { return "rest_articles" }

const articleDDL = `
CREATE TABLE rest_articles (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT ''
);
ALTER TABLE rest_articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_articles FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_articles_tenant ON rest_articles
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// spokeTenant is the tenant these cases resolve to. It declares Portuguese,
// which is what makes a request for pt-PT a question this installation answers.
var spokeTenant = tenancy.Tenant{
	ID: uuid.New(), Slug: "polyglot", Name: "Polyglot",
	Languages: &tenancy.Languages{Default: "en", Others: []string{"pt-PT"}},
}

type spokeCaller struct{}

func (spokeCaller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return spokeTenant, nil
}
func (spokeCaller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// stubPort is the translation module as the read door sees it: one reviewed row
// for the title, no row for the body, and the queries it was asked kept, so a
// case can check the door handed over the source it was holding rather than
// leaving the port to guess what the record says now.
type stubPort struct {
	asked []rest.TranslatedQuery
	rows  map[string]rest.TranslatedField
}

func (p *stubPort) Translated(_ context.Context, _ db.Tx[db.Tenant], q rest.TranslatedQuery) ([]rest.TranslatedRecord, error) {
	p.asked = append(p.asked, q)
	out := make([]rest.TranslatedRecord, 0, len(q.RecordIDs))
	for _, recID := range q.RecordIDs {
		fields := map[string]rest.TranslatedField{}
		for name, f := range p.rows {
			if _, has := q.Sources[recID][name]; has {
				fields[name] = f
			}
		}
		out = append(out, rest.TranslatedRecord{ID: recID, Fields: fields})
	}
	return out, nil
}

func (p *stubPort) Save(context.Context, db.Tx[db.Tenant], rest.SaveQuery) error { return nil }
func (p *stubPort) Review(context.Context, db.Tx[db.Tenant], rest.ReviewQuery) error {
	return nil
}
func (p *stubPort) Suggest(context.Context, db.Tx[db.Tenant], rest.SuggestQuery) error {
	return nil
}
func (p *stubPort) Untranslate(context.Context, db.Tx[db.Tenant], rest.ReviewQuery) error {
	return nil
}
func (p *stubPort) Overview(context.Context, db.Tx[db.Tenant], rest.OverviewQuery) ([]rest.OverviewRow, rest.OverviewCounts, int64, error) {
	return nil, rest.OverviewCounts{}, 0, nil
}
func (p *stubPort) ForgetRecord(context.Context, db.Tx[db.Tenant], string, string, uuid.UUID) error {
	return nil
}
func (p *stubPort) MarkOutdated(context.Context, db.Tx[db.Tenant], string, string, string, uuid.UUID) error {
	return nil
}

var articlePort = &stubPort{rows: map[string]rest.TranslatedField{
	// Reviewed Portuguese of the title: no status, because there is nothing to
	// warn about, and the value replaces the source.
	"title": {Value: "Sobre nós.", Origin: rest.OriginHuman, Revision: 3},
}}

// mountedArticle is the article Spec behind the whole middleware chain, over the
// given port, with one row created through the resource's own door.
func mountedArticle(t *testing.T, port rest.Translations) (chi.Router, string) {
	t.Helper()
	s := rest.Spec[*Article]{
		Module: "articles", Entity: "article", Path: "/article",
		Read: "article:read", Write: "article:write", Translations: port,
	}
	_, router, admin := mountAs(t, s, spokeCaller{})
	// The table is this module's own migration's business in the application;
	// here it is one CREATE before the first request, on the admin handle the
	// harness hands back. The row is created through the resource's own door.
	if _, err := admin.ExecContext(t.Context(), articleDDL); err != nil {
		t.Fatalf("create rest_articles: %v", err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article",
		`{"title":"About us.","body":"Our story."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the article = %d %s, want 201", code, body)
	}
	return router, id(t, body)
}

// callHead is call, with one response header kept: the language a response is
// written in travels in a header, so a case that cannot read headers cannot ask
// whether the door said it.
func callHead(t *testing.T, r http.Handler, method, path string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header()
}

// TestTheItemDoorAnswersInTheLanguageItWasAskedFor: the translated value is in
// the field, the field with no row is named in `_i18n` as the source it is, and
// the response says which language it is written in.
func TestTheItemDoorAnswersInTheLanguageItWasAskedFor(t *testing.T) {
	router, articleID := mountedArticle(t, articlePort)

	code, body, head := callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID+"?lang=pt-PT")
	if code != http.StatusOK {
		t.Fatalf("GET ?lang=pt-PT = %d %s", code, body)
	}
	if got := head.Get("Content-Language"); got != "pt-PT" {
		t.Errorf("Content-Language = %q, want pt-PT: the body is in Portuguese and the response says nothing", got)
	}
	if !strings.Contains(body, `"title":"Sobre nós."`) {
		t.Errorf("the title was not overlaid: %s", body)
	}
	if !strings.Contains(body, `"body":"Our story."`) {
		t.Errorf("a field with no translation must keep the source: %s", body)
	}
	if !strings.Contains(body, `"_i18n"`) || !strings.Contains(body, `"body"`) ||
		!strings.Contains(body, `"status":"missing"`) || !strings.Contains(body, `"locale":"en"`) {
		t.Errorf("_i18n does not name the field that fell back: %s", body)
	}
	var i18n map[string]map[string]string
	if err := json.Unmarshal([]byte(mustField(t, body, "_i18n")), &i18n); err != nil {
		t.Fatalf("_i18n is not the object the response promises: %v in %s", err, body)
	}
	if _, named := i18n["title"]; named {
		t.Errorf("_i18n names title, which is in the requested language: %s", body)
	}
	if _, named := i18n["body"]; !named {
		t.Errorf("_i18n does not name body, which is not: %s", body)
	}
	if len(articlePort.asked) == 0 {
		t.Fatal("the read never asked the translation port anything")
	}
	last := articlePort.asked[len(articlePort.asked)-1]
	if last.Locale != "pt-PT" || last.Module != "articles" || last.Entity != "article" {
		t.Errorf("the port was asked %s.%s in %q, want articles.article in pt-PT", last.Module, last.Entity, last.Locale)
	}
	if got := last.Sources[uuid.MustParse(articleID)]["title"]; got != "About us." {
		t.Errorf("the port was handed source %q, want the text the record holds: the staleness rule compares against this", got)
	}
	if last.Public {
		t.Error("a permission-guarded door asked as the public one, which would withhold the tenant's own reviewed text")
	}
}

// TestAReadInTheTenantsOwnLanguageSaysSoAndAsksNothing: the default language is
// not a translation question, and a language the tenant never declared is not
// either. Both answers are "the source, and no claim about a language".
func TestAReadTheTenantWasNeverInIsNotATranslationQuestion(t *testing.T) {
	empty := &stubPort{}
	router, articleID := mountedArticle(t, empty)

	before := len(empty.asked)
	code, body, head := callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID)
	if code != http.StatusOK {
		t.Fatalf("GET with no lang = %d %s", code, body)
	}
	if got := head.Get("Content-Language"); got != "" {
		t.Errorf("Content-Language = %q on a read that asked for no language", got)
	}
	if strings.Contains(body, "_i18n") {
		t.Errorf("_i18n appeared on a response with no fallback in it: %s", body)
	}

	// The tenant's own language: answered, and asked of nobody.
	code, body, head = callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID+"?lang=en")
	if code != http.StatusOK || head.Get("Content-Language") != "en" {
		t.Fatalf("GET ?lang=en = %d, Content-Language %q, want 200 and en", code, head.Get("Content-Language"))
	}
	if !strings.Contains(body, `"title":"About us."`) {
		t.Errorf("the default language lost its own text: %s", body)
	}

	// A language nobody declared: the request is ignored rather than honoured.
	code, body, head = callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID+"?lang=de-DE")
	if code != http.StatusOK {
		t.Fatalf("GET ?lang=de-DE = %d %s", code, body)
	}
	if got := head.Get("Content-Language"); got != "" {
		t.Errorf("Content-Language = %q for a language this tenant does not speak", got)
	}
	if !strings.Contains(body, `"title":"About us."`) {
		t.Errorf("an undeclared language changed the body: %s", body)
	}
	if len(empty.asked) != before {
		t.Errorf("the port was asked %d more times for reads in the default and an undeclared language, want 0",
			len(empty.asked)-before)
	}
}

// TestTheListDoorOverlaysTheSameRows: the collection answers the same way, and
// the header is one sentence about the whole page.
func TestTheListDoorOverlaysTheSameRows(t *testing.T) {
	router, _ := mountedArticle(t, articlePort)
	code, body, head := callHead(t, router, http.MethodGet, "/api/v1/articles/article?lang=pt-PT")
	if code != http.StatusOK {
		t.Fatalf("GET the list ?lang=pt-PT = %d %s", code, body)
	}
	if got := head.Get("Content-Language"); got != "pt-PT" {
		t.Errorf("the list's Content-Language = %q, want pt-PT", got)
	}
	if !strings.Contains(body, `"title":"Sobre nós."`) {
		t.Errorf("the list never overlaid its rows: %s", body)
	}
}

// TestThePlainResourceNeitherOffersNorReadsLang is the third way this feature
// could lie: a parameter on every door in the installation, read by none of
// them. The Task Spec declares no translatable field, so its document carries no
// lang parameter, and its answer does not change when one is sent anyway.
func TestThePlainResourceNeitherOffersNorReadsLang(t *testing.T) {
	_, router, _ := mounted(t)
	code, plain, head := callHead(t, router, http.MethodGet, "/api/v1/tasks/task")
	if code != http.StatusOK {
		t.Fatalf("GET the plain list = %d %s", code, plain)
	}
	if got := head.Get("Content-Language"); got != "" {
		t.Errorf("the plain resource set Content-Language %q, which it has no way to know", got)
	}
	_, withParam, _ := callHead(t, router, http.MethodGet, "/api/v1/tasks/task?lang=pt-PT")
	if withParam != plain {
		t.Errorf("?lang= moved the plain resource's body:\n  %s\n  %s", plain, withParam)
	}
}

// mustField is one top-level member of a JSON object, as raw bytes, for a case
// that wants to look inside one member of a response without declaring the whole
// entity a second time.
func mustField(t *testing.T, body, field string) string {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("the response is not an object: %v in %s", err, body)
	}
	raw, ok := out[field]
	if !ok {
		t.Fatalf("the response carries no %q: %s", field, body)
	}
	return string(raw)
}
