package main

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// TestEveryAdminPagePseudoLocalisesEveryWord: rendered in the pseudo-locale en-XA
// (accented and expanded English), an admin page carries no unaccented English
// word, because every visible string came through the catalogue. The exceptions
// are data, not copy, and each says why: the tenant's name and the installation's
// brand are what an operator typed; an account label is an id and a role name;
// language tags, a language's own name and addresses are values, not words.
func TestEveryAdminPagePseudoLocalisesEveryWord(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	id := installationTenantID(t, cfg, admin, "acme")
	// Whether en-XA is a language a tenant declares or one the installation answers
	// on the browser's ranking alone is the cure's choice: try the declaration, and
	// hold the page to what it says it is either way.
	declared, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id.String()+"/locale",
		`{"default":"en","supported":["en-XA","pt-PT"]}`)
	data := map[string]bool{"Acme Corporation": true, "PlatformKit (development)": true,
		"admin": true, "en": true, "en-XA": true, "pt-PT": true}
	// An account label is a short id and a role name, and a row whose entity declares
	// no readable column is linked by its uuid. Both are identifiers, which the brief
	// names as data rather than copy — and a uuid is hex, so a three-letter letter run
	// inside one ("…-bced-…") is a fact about the random id, not a word no catalogue
	// was asked for. Anchored on the whole segment, so no sentence can hide here.
	identifier := regexp.MustCompile(`^(?:[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}|[0-9a-f]{8} · \S+)$`)
	unaccented := regexp.MustCompile(`\b[A-Za-z]{3,}\b`)
	hidden := regexp.MustCompile(`(?s)<(script|style|svg)\b.*?</(script|style|svg)>`)
	tag := regexp.MustCompile(`(?s)<[^>]*>`)
	// A language is named in its own language wherever a person picks one, so the
	// names of the languages offered are endonyms and not copy to pseudo-localise.
	endonyms := map[string]bool{}
	for _, tag := range []string{"en", "pt-PT", "en-XA"} {
		endonyms[display.Self.Name(language.MustParse(tag))] = true
	}
	for _, path := range []string{"/app", "/app/user/users", "/app/task/tasks/new", "/app/tenant/languages"} {
		code, _, _, page := r7navigate(t, cfg, admin, acmeHost, path, "en-XA")
		if code != http.StatusOK || pageLang(page) != "en-XA" {
			t.Fatalf("%s answered %d with lang=%q, want an en-XA page (declaring en-XA answered %d: %s)",
				path, code, pageLang(page), declared, refusal)
		}
		body := hidden.ReplaceAllString(page[strings.Index(page, "<body"):], " ")
		for _, segment := range tag.Split(body, -1) {
			segment = strings.Join(strings.Fields(html.UnescapeString(segment)), " ")
			if segment == "" || data[segment] || endonyms[segment] || identifier.MatchString(segment) || strings.Contains(segment, "@") {
				continue
			}
			if word := unaccented.FindString(segment); word != "" {
				t.Errorf("%s in en-XA writes the untranslated %q in %q", path, word, segment)
			}
		}
	}
}
