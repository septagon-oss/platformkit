package page_test

// A reviewer's cases, T-0111 review round 2 — the cure for round 1's Finding 1
// (`ui/page/review_round1_preference_order_test.go`) under mutation.
//
// Round 1 found that cutting the `Accept-Language` header into tokens to filter it
// threw away the caller's ranking, because `language.MatchStrings` takes the first
// entry it is given that matches anything at all and only one entry is sorted by q.
// The cure is to re-join the surviving tokens into one header so the provider sorts
// them again. `TestTheSameRequestWrittenInEitherOrderIsAnsweredInOneLanguage` below
// is that property, stated as the property rather than as three rows: the answer is
// a function of what the header said, not of the order it happened to be serialised
// in. It passes on this branch.
//
// `TestABrowserOfferingARegionalSpellingOfATenantLanguageIsAnsweredInThatLanguage`
// is the hole the filter's own rule leaves. `speaks` keeps a token when it equals a
// supported tag, or when one is a hyphen-prefix of the other — which is why bare
// `pt` is kept for a tenant that serves `pt-PT`, and the file says so in so many
// words ("that is what 'this browser speaks Portuguese' means"). A browser offering
// `pt-BR` to that same tenant is refused by the same rule: `pt-BR` is neither equal
// to `pt-PT` nor a hyphen-prefix of it. So the tenant's filter keeps the *more
// distant* spelling (the language alone) and drops the *nearer* one (the same
// language, a different region), and the pt-BR browser falls through to the
// tenant's default. On `main`, where the whole header reached the provider, the
// same request was answered from the Portuguese copy: `before` below is that call,
// verbatim, and the assertion is that the tenant's filter did not move it.

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// speaksBoth is a deployment with two catalogues and a tenant served in both, en
// being the language its copy is written in.
func speaksBoth(t *testing.T, served *tenancy.Languages) func(header string) (before, after string) {
	t.Helper()
	messages := xtext.Load("en", page.Catalogue(), xtext.Source{
		Name: "the case",
		FS: fstest.MapFS{
			"en.json":    &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Edit"}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Editar"}}`)},
		},
	})
	r := page.Request{Tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Languages: served}}
	return func(header string) (string, string) {
		return page.SelectLocale(messages, "", header).Language,
			page.SelectLocale(messages, page.TenantPreferences(r, "", header)...).Language
	}
}

// TestABrowserOfferingARegionalSpellingOfATenantLanguageIsAnsweredInThatLanguage:
// the tenant serves pt-PT, the browser speaks a Portuguese that is not Portugal's.
func TestABrowserOfferingARegionalSpellingOfATenantLanguageIsAnsweredInThatLanguage(t *testing.T) {
	ask := speaksBoth(t, &tenancy.Languages{Default: "en", Others: []string{"pt-PT"}})
	for _, c := range []struct{ header, want string }{
		{"pt-BR", "pt-PT"},          // a Brazilian browser: Portuguese copy, not the tenant's default
		{"pt-BR;q=0.9", "pt-PT"},    // the same, with a quality value on it
		{"pt-MO,pt;q=0.9", "pt-PT"}, // Macao Portuguese, with the bare-language fallback
		{"en-GB", "en"},             // and the case the filter already carries
	} {
		before, after := ask(c.header)
		if before != c.want {
			t.Fatalf("the answer the same request got before the tenant had a say = %q, want %q: "+
				"the comparison below means nothing if this moved", before, c.want)
		}
		if after != c.want {
			t.Errorf("a browser offering %q to a tenant served in pt-PT was answered in %q, want %q: "+
				"`speaks` keeps bare pt for a tenant that serves pt-PT, so a Portuguese of another "+
				"region is no further from what this tenant declared it speaks", c.header, after, c.want)
		}
	}
}

// TestTheSameRequestWrittenInEitherOrderIsAnsweredInOneLanguage is round 1's
// finding as a rule: two serialisations of one set of preferences are one request,
// and the tenant's filter must not turn them into two answers.
func TestTheSameRequestWrittenInEitherOrderIsAnsweredInOneLanguage(t *testing.T) {
	served := &tenancy.Languages{Default: "en", Others: []string{"pt-PT"}}
	ask := speaksBoth(t, served)
	for _, pair := range [][2]string{
		{"en;q=0.9,pt-PT;q=0.1", "pt-PT;q=0.1,en;q=0.9"},
		{"en;q=0.1,pt-PT;q=0.9", "pt-PT;q=0.9,en;q=0.1"},
		{"de;q=0.9,pt;q=0.5,en;q=0.1", "en;q=0.1,de;q=0.9,pt;q=0.5"},
		{"de;q=0.9,en;q=0.5,pt;q=0.1", "pt;q=0.1,en;q=0.5,de;q=0.9"},
	} {
		_, first := ask(pair[0])
		_, second := ask(pair[1])
		if first != second {
			t.Errorf("%q and %q are one request written in two orders and were answered %q and %q: "+
				"the tenant's set survived its filter, its caller's ranking did not",
				pair[0], pair[1], first, second)
		}
	}
}

// TestATenantDeclaredInALanguageTheDeploymentNoLongerCarriesIsDeclaredInOneItDoes
// pins the other half of round 1's Finding 3: a tenant whose declaration outlives
// the catalogue behind it must be answered — and must declare itself — in a language
// its copy actually exists in. The reference composition answers in the source
// language when nothing matches; what must never happen is a page that says
// `lang="pt-PT"` over copy no Portuguese file was ever read for.
func TestATenantDeclaredInALanguageTheDeploymentNoLongerCarriesIsDeclaredInOneItDoes(t *testing.T) {
	english := xtext.Load("en", xtext.Source{
		Name: "en only",
		FS:   fstest.MapFS{"en.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Edit"}}`)}},
	})
	r := page.Request{Tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme",
		Languages: &tenancy.Languages{Default: "pt-PT"}}}
	got := page.SelectLocale(english, page.TenantPreferences(r, "pt-PT")...)
	if slices.Contains(english.Languages(), got.Language) {
		return // a deployment that does carry it has nothing to say here
	}
	if got.Language != "en" {
		t.Errorf("a tenant declared %q but the deployment carries %v; the request was answered in %q, "+
			"want the language there is copy in", "pt-PT", english.Languages(), got.Language)
	}
}
