package rest_test

// translations_write_door_test.go pins the write side of the translation door:
// the record's own commands write one language's text, the field's own rules
// refuse what the source could never be saved as, and the source the port is
// handed is the text the record holds under its own row lock.
//
// Four arms matter most, and each is a way the feature could lie without anybody
// noticing at the door:
//
//   - A translation write that moved the record's own field. The English would
//     become Portuguese, in the row, for everybody.
//   - A language the tenant is never served in, or its own: the first files text
//     no reader can be routed to, the second overwrites the source with a
//     translation of it.
//   - A translated body that would be refused in English and is accepted because
//     it arrived in Portuguese. The same rules, on the same door, is the claim.
//   - A door guarded by nothing, or by a permission of its own. A translation of
//     a page is written by whoever may write the page — so the recorded
//     operation has to name the record's write permission, which is asserted here
//     rather than assumed, because "the record's permission guards it" is a
//     sentence that is true only while somebody checks it.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

// recordedPort is the translation module as seen by a door that is being asked
// what it handed over: the reads fall through to the stub the read-door cases
// already use, and every write is kept.
type recordedPort struct {
	rest.Translations
	saved     []rest.SaveQuery
	reviewed  []rest.ReviewQuery
	suggested []rest.SuggestQuery
	removed   []rest.ReviewQuery
}

func (p *recordedPort) Save(_ context.Context, _ db.Tx[db.Tenant], q rest.SaveQuery) error {
	p.saved = append(p.saved, q)
	return nil
}

func (p *recordedPort) Review(_ context.Context, _ db.Tx[db.Tenant], q rest.ReviewQuery) error {
	p.reviewed = append(p.reviewed, q)
	return nil
}

func (p *recordedPort) Suggest(_ context.Context, _ db.Tx[db.Tenant], q rest.SuggestQuery) error {
	p.suggested = append(p.suggested, q)
	return nil
}

func (p *recordedPort) Untranslate(_ context.Context, _ db.Tx[db.Tenant], q rest.ReviewQuery) error {
	p.removed = append(p.removed, q)
	return nil
}

// Slide is the richtext arm: one translatable field with a ceiling, so the door's
// own rules have something to refuse.
type Slide struct {
	entity.Base
	Caption string `json:"caption" gorm:"type:text" ui:"widget:richtext" maxLength:"20" i18n:"translatable"`
}

func (Slide) TableName() string { return "rest_slides" }

const slideDDL = `
CREATE TABLE rest_slides (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	caption text NOT NULL DEFAULT ''
);
ALTER TABLE rest_slides ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_slides FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_slides_tenant ON rest_slides
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

func mountedSlide(t *testing.T, port rest.Translations) (chi.Router, string) {
	t.Helper()
	s := rest.Spec[*Slide]{
		Module: "slides", Entity: "slide", Path: "/slide",
		Read: "slide:read", Write: "slide:write", Translations: port,
		RichTextFiles: richtext.RejectImages{},
	}
	_, router, admin := mountAs(t, s, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), slideDDL); err != nil {
		t.Fatalf("create rest_slides: %v", err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/slides/slide", `{"caption":"Our story."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the slide = %d %s, want 201", code, body)
	}
	return router, id(t, body)
}

// TestTheRecordDoorWritesTheTranslationAndNotTheRecord: the Portuguese lands in
// the port, the source it was measured against is the text the row holds, and the
// record's own column still says what it said in English.
func TestTheRecordDoorWritesTheTranslationAndNotTheRecord(t *testing.T) {
	port := &recordedPort{Translations: articlePort}
	router, articleID := mountedArticle(t, port)

	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/translate",
		`{"lang":"pt-PT","values":{"title":"Sobre nós."},"expected":{"title":3}}`)
	if code != http.StatusOK {
		t.Fatalf("POST translate = %d %s, want 200", code, body)
	}
	if len(port.saved) != 1 {
		t.Fatalf("the door wrote %d translations, want exactly one", len(port.saved))
	}
	q := port.saved[0]
	if q.Module != "articles" || q.Entity != "article" || q.Locale != "pt-PT" {
		t.Errorf("the port was asked %s.%s in %q, want articles.article in pt-PT", q.Module, q.Entity, q.Locale)
	}
	if q.RecordID != uuid.MustParse(articleID) {
		t.Errorf("the translation is of %s, want %s", q.RecordID, articleID)
	}
	if q.Values["title"] != "Sobre nós." {
		t.Errorf("the value saved is %q, want the text the caller typed", q.Values["title"])
	}
	// The source is the door's answer, not the port's guess: the staleness rule
	// and the side-by-side view both read the pair Save stores from this map.
	if q.Source["title"] != "About us." || q.Source["body"] != "Our story." {
		t.Errorf("the port was handed source %v, want the text the record holds under its lock", q.Source)
	}
	if q.Origin != rest.OriginHuman {
		t.Errorf("origin %q: a person typed this and the row must say so", q.Origin)
	}
	if q.Expected["title"] != 3 {
		t.Errorf("expected revision %d, want the 3 the caller read: without it a second translator overwrites the first", q.Expected["title"])
	}

	// The record, asked in its own language, is what it always was.
	code, body = call(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID, "")
	if code != http.StatusOK || !strings.Contains(body, `"title":"About us."`) {
		t.Errorf("the translation write moved the record itself: %d %s", code, body)
	}
}

// TestALanguageTheTenantIsNeverServedInIsRefused, and with it the tenant's own
// language: the first would file text no reader can be routed to, the second is a
// request to overwrite the source with a translation of it. Both write nothing.
func TestALanguageTheTenantIsNeverServedInIsRefused(t *testing.T) {
	for _, lang := range []string{"de-DE", "", "en"} {
		port := &recordedPort{Translations: articlePort}
		router, articleID := mountedArticle(t, port)
		code, body := call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/translate",
			`{"lang":"`+lang+`","values":{"title":"Nein."}}`)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("translate in %q = %d %s, want 422", lang, code, body)
		}
		if len(port.saved) != 0 {
			t.Errorf("translate in %q saved %v, want nothing written", lang, port.saved)
		}
	}
}

// TestAFieldThatIsNotTranslatableIsRefused: a body naming a field the entity does
// not declare, or declares without the tag, is refused rather than dropped — a
// translation stored under a name no read asks for is a row nothing can find,
// show or delete.
func TestAFieldThatIsNotTranslatableIsRefused(t *testing.T) {
	for _, field := range []string{"nope", "createdAt"} {
		port := &recordedPort{Translations: articlePort}
		router, articleID := mountedArticle(t, port)
		code, body := call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/translate",
			`{"lang":"pt-PT","values":{"`+field+`":"Não."}}`)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("translate %s = %d %s, want 422", field, code, body)
		}
		if len(port.saved) != 0 {
			t.Errorf("translate %s saved %v, want nothing written", field, port.saved)
		}
	}
}

// TestATranslatedRichTextConstructIsRefusedAtTheDoor: the field's rules are the
// field's, in every language. A body over the ceiling is 422 with the remedy in
// it, and the accepted one is stored normalised, which is why the door hands over
// what richtext prepared rather than what the form sent.
func TestATranslatedRichTextConstructIsRefusedAtTheDoor(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, slideID := mountedSlide(t, port)

	tooLong := strings.Repeat("traduzido ", 5) // 50 characters against a ceiling of 20
	code, body := call(t, router, http.MethodPost, "/api/v1/slides/slide/"+slideID+"/translate",
		`{"lang":"pt-PT","values":{"caption":"`+tooLong+`"}}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("translate a 50-character caption = %d %s, want 422", code, body)
	}
	if !strings.Contains(body, "length") || !strings.Contains(body, "20") {
		t.Errorf("the refusal says nothing about the ceiling it hit: %s", body)
	}
	if len(port.saved) != 0 {
		t.Fatalf("the refused translation wrote %v", port.saved)
	}

	code, body = call(t, router, http.MethodPost, "/api/v1/slides/slide/"+slideID+"/translate",
		`{"lang":"pt-PT","values":{"caption":"A nossa história.\n\n\n"}}`)
	if code != http.StatusOK {
		t.Fatalf("translate a valid caption = %d %s, want 200", code, body)
	}
	if len(port.saved) != 1 {
		t.Fatalf("the accepted translation wrote %d rows-worth, want one", len(port.saved))
	}
	// Canonical, by the format's own rule and not by a spelling copied here: what
	// the door stores is what richtext prepared, which is what the source row
	// stores for the same field typed in English.
	want, err := richtext.Normalise("A nossa história.\n\n\n")
	if err != nil {
		t.Fatalf("normalise the accepted caption: %v", err)
	}
	if got := port.saved[0].Values["caption"]; got != want {
		t.Errorf("stored caption %q, want the normalised text %q: two spellings of one document are two translations", got, want)
	}
	if !port.saved[0].RichText["caption"] {
		t.Errorf("the port was told caption is plain text, and would hash it the wrong way: %v", port.saved[0].RichText)
	}
}

// TestTheReviewAndSuggestDoorsHandOverTheSourceTheyAreHolding: a review is a claim
// about the source, and the empty field list means every field of the record — so
// the door has to arrive with the record's current text and the language it was
// asked in, and the machine has to be told which language it is translating out
// of.
func TestTheReviewAndSuggestDoorsHandOverTheSourceTheyAreHolding(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)

	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/review-translation",
		`{"lang":"pt-PT"}`)
	if code != http.StatusOK {
		t.Fatalf("POST review-translation = %d %s, want 200", code, body)
	}
	if len(port.reviewed) != 1 {
		t.Fatalf("review wrote %d queries, want one", len(port.reviewed))
	}
	q := port.reviewed[0]
	if len(q.Fields) != 0 {
		t.Errorf("an empty body named fields %v, want the empty answer that means every field", q.Fields)
	}
	if q.Source["title"] != "About us." {
		t.Errorf("review was asked with source %v, want the record's own text", q.Source)
	}

	code, body = call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/suggest-translation",
		`{"lang":"pt-PT","fields":["body"]}`)
	if code != http.StatusOK {
		t.Fatalf("POST suggest-translation = %d %s, want 200", code, body)
	}
	if len(port.suggested) != 1 {
		t.Fatalf("suggest wrote %d queries, want one", len(port.suggested))
	}
	if got := port.suggested[0]; got.From != "en" || got.Locale != "pt-PT" || !slices.Equal(got.Fields, []string{"body"}) {
		t.Errorf("suggest asked for %q out of %q for %v, want body out of en into pt-PT", got.Locale, got.From, got.Fields)
	}

	code, body = call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/untranslate",
		`{"lang":"pt-PT","fields":["title"]}`)
	if code != http.StatusOK {
		t.Fatalf("POST untranslate = %d %s, want 200", code, body)
	}
	if len(port.removed) != 1 || port.removed[0].Locale != "pt-PT" || !slices.Equal(port.removed[0].Fields, []string{"title"}) {
		t.Errorf("untranslate reached the port as %v, want title in pt-PT", port.removed)
	}
}

// TestEachTranslationDoorCarriesTheRecordsOwnWritePermission: the module declares
// no permission, and the sentence is only honest while the mount carries the
// record's. A caller the router would refuse for the PATCH must be refused for the
// translation, at the same declared guard.
func TestEachTranslationDoorCarriesTheRecordsOwnWritePermission(t *testing.T) {
	api, _, _ := mountAs(t, rest.Spec[*Article]{
		Module: "articles", Entity: "article", Path: "/article",
		Read: "article:read", Write: "article:write", Translations: &stubPort{},
	}, spokeCaller{})
	for _, verb := range []string{"translate", "review-translation", "suggest-translation", "untranslate"} {
		op := recorded(t, api, "articles-article-"+verb)
		if op == nil {
			t.Errorf("POST …/article/{id}/%s is not mounted", verb)
			continue
		}
		auth, _ := op.Extensions[httpx.AuthExtension].(httpx.Auth)
		if auth.String() != "permission article:write" {
			t.Errorf("%s declares %s, want permission article:write", verb, auth.String())
		}
	}
}

// TestAPlainResourceMountsNoTranslationDoor: the parameter and the commands exist
// only where something can answer them. A task has no translatable field, so a
// door that could only refuse it would be a door the catalog lies about.
func TestAPlainResourceMountsNoTranslationDoor(t *testing.T) {
	api, router, _ := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"Ship it"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the task = %d %s, want 201", code, body)
	}
	taskID := id(t, body)
	if op := recorded(t, api, "tasks-task-read"); op == nil {
		t.Fatal("the plain resource's own read door is not recorded: recorded() would answer nil for everything")
	}
	for _, verb := range []string{"translate", "review-translation", "suggest-translation", "untranslate"} {
		if op := recorded(t, api, "tasks-task-"+verb); op != nil {
			t.Errorf("a resource with no translatable field mounts %s", verb)
		}
		code, _ = call(t, router, http.MethodPost, "/api/v1/tasks/task/"+taskID+"/"+verb, `{"lang":"pt-PT"}`)
		if code == http.StatusOK || code == http.StatusCreated {
			t.Errorf("POST …/task/{id}/%s = %d, want the door not to be there at all", verb, code)
		}
	}
}
