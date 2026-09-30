package page_test

// A reviewer's case, T-0111 review round 1.
//
// `TenantPreferences` splits the request's Accept-Language into tokens so it can
// drop the ones this tenant does not serve, and hands the provider a *list*. The
// file says the quality value "stays on what is passed on: the provider reads it"
// — and the provider does read it, but only inside one token: `language.MatchStrings`
// walks the list it is given and returns the first token that matches anything at
// all. The tokens are in the order the header wrote them, not in the order the
// caller meant, so the q values decide nothing once the header is cut up.
//
// What a person gets is the tenant's default instead of the language they asked
// for first. The two rows below are the same request written in the two orders;
// the third is a browser that asked for German first (which this tenant does not
// serve) and Portuguese at 0.1 against English at 0.5 — it is answered in the
// language it ranked *lowest* of the two it offered.
//
// This is a change of behaviour against `main`, where the whole header reached the
// provider as one string, `language.ParseAcceptLanguage` sorted it by q, and the
// same request was answered in English. `before` below is that call, verbatim, so
// the two answers can be read side by side.
//
// The assertion is about the fixed behaviour — the language the caller ranked
// highest among the ones this tenant serves — so it goes green when the ordering
// is restored, whichever way it is restored (sort the surviving tokens by q, or
// filter the tags inside the one header string instead of rebuilding a list).

import (
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// servesIn is the deployment's two catalogues — the kernel's own, and one pair of
// files carrying the label a generated screen prints — with a tenant that declared
// it is served in both.
func servesIn(t *testing.T, languages *tenancy.Languages) func(header string, before, want string) {
	t.Helper()
	messages := xtext.Load("en", page.Catalogue(), xtext.Source{
		Name: "the case",
		FS: fstest.MapFS{
			"en.json":    &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Edit"}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Editar"}}`)},
		},
	})
	r := page.Request{Tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Languages: languages}}
	return func(header, before, want string) {
		t.Helper()
		if got := page.SelectLocale(messages, "", header).Language; got != before {
			t.Fatalf("the answer a request got before the tenant had a say = %q, want %q: "+
				"the comparison below means nothing if this moved too", got, before)
		}
		got := page.SelectLocale(messages, page.TenantPreferences(r, header)...).Language
		if got != want {
			t.Errorf("a browser offering %q to a tenant served in pt-PT and en was answered in %q, want %q: "+
				"the tenant's set was honoured, its ranking was thrown away", header, got, want)
		}
	}
}

func TestTheLanguageABrowserRankedFirstIsTheOneItIsAnsweredIn(t *testing.T) {
	served := &tenancy.Languages{Default: "pt-PT", Others: []string{"en"}}
	ask := servesIn(t, served)
	ask("en;q=0.9,pt-PT;q=0.1", "en", "en")
	ask("pt-PT;q=0.1,en;q=0.9", "en", "en")
	ask("de-DE,de;q=0.9,pt;q=0.1,en;q=0.5", "en", "en")
}

// TestATenantServedInOneLanguageAnswersEveryOtherRequestWithItsDefault is the half
// that must keep working: a tenant that serves only Portuguese answers a browser
// that asked for English in Portuguese, in every order the header can be written in.
func TestATenantServedInOneLanguageAnswersEveryOtherRequestWithItsDefault(t *testing.T) {
	ask := servesIn(t, &tenancy.Languages{Default: "pt-PT"})
	ask("en-GB,en;q=0.9", "en", "pt-PT")
	ask("de-DE,de;q=0.9,en;q=0.8", "en", "pt-PT")
	ask("en;q=0.9,pt-PT;q=0.1", "en", "pt-PT")
}
