package page_test

// The reviewer's case for the second review of T-0024: the language a refusal page is
// written in. The brief specifies this piece of the change in two places — §"Locale,
// time, money" ("refusal pages stop being English-only": httpx publishes the refusal
// codes, ui/page holds one table code → "fault.<code>" catalog key, fault() takes the
// resolved language and stops pinning "en", the page carries Content-Language and
// Vary: Accept-Language) and §9's test 16, TestRefusalPagesSpeakBothLanguages. The
// change published the codes (kit/httpx/middleware.go:1089) and added the sentence this
// case holds it to — the same file, added by this commit: "ui/page holds the one table
// from a code to a translation key, so a deployment that ships two languages answers a
// refusal in both". No file under ui/ names a refusal code or a "fault." key
// (git grep -n '"fault\.' finds nothing), ui/page/fault.go:43 still calls
// fault(status, p.Detail, …) without the resolved locale, ui/document.Fault still
// returns View{Language: "en"}, and FaultHandler writes only Content-Type and
// Cache-Control — while Serve writes Content-Language and Vary (serve.go:123).
//
// Reviewer: a fresh pi session, 2026-09-21.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"

	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/page"
)

const (
	deniedInEnglish    = "This request came from another site, so nothing was written."
	deniedInPortuguese = "Este pedido veio de outro site, por isso nada foi escrito."
)

// twoLanguageFaultShell is the shell the brief describes: the foundation's test catalog
// ships en and pt-PT, and it carries the sentence for the code the kernel published.
func twoLanguageFaultShell(t *testing.T) page.Shell {
	t.Helper()
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	if err := messages.SetString(language.English, "fault.CSRF_ORIGIN", deniedInEnglish); err != nil {
		t.Fatal(err)
	}
	if err := messages.SetString(language.EuropeanPortuguese, "fault.CSRF_ORIGIN", deniedInPortuguese); err != nil {
		t.Fatal(err)
	}
	s := shell()
	s.Messages = page.FromCatalog(messages)
	return s
}

func csrfRefusal() *problem.Problem {
	return &problem.Problem{
		Status:   http.StatusForbidden,
		Detail:   "CSRF_ORIGIN: this request carries a session cookie and came from another site",
		Instance: "urn:request:1111aaaa-2222-3333-4444-555566667777",
	}
}

// TestARefusalPageSpeaksTheLanguageTheRequestAskedFor. The refusal's second shape (ADR
// 0015) is a sentence for a person, and the person's browser said Portuguese. The
// passing branch is the one the brief specifies and the added comment claims stands:
// the code names a catalog key, the page is written in the negotiated language, and it
// says so in both the attribute and the header.
func TestARefusalPageSpeaksTheLanguageTheRequestAskedFor(t *testing.T) {
	s := twoLanguageFaultShell(t)

	// The fixture speaks Portuguese, independently of anything under test: the same
	// catalog and the same preferences are what every page of this shell already
	// negotiates. True today and after any fix.
	if got := page.SelectLocale(s.Messages, "pt-PT, en;q=0.8").Language; got != "pt-PT" {
		t.Fatalf("this shell does not speak Portuguese at all: negotiated %q", got)
	}

	req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/tasks", nil)
	req.Header.Set("Accept-Language", "pt-PT, en;q=0.8")
	got := renderFault(t, s, csrfRefusal(), req)
	body := got.Body.String()

	if !strings.Contains(body, deniedInPortuguese) {
		t.Errorf("a refusal of a pt-PT request is written in English; the catalog holds %q for the code the "+
			"kernel published, and the table from a code to that key is what ui/page is said to hold: %s",
			deniedInPortuguese, firstBytes(body))
	}
	if !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("the refusal document declares no Portuguese although its shell negotiated it; document.Fault "+
			"pins Language:\"en\", which the brief asks this change to stop doing: %s", firstBytes(body))
	}
	if got.Header().Get("Content-Language") != "pt-PT" {
		t.Errorf("the refusal page sends Content-Language %q; FaultHandler writes only Content-Type and "+
			"Cache-Control, while Serve sends the language and Vary (ui/page/serve.go:123)",
			got.Header().Get("Content-Language"))
	}
	if !strings.Contains(got.Header().Get("Vary"), "Accept-Language") {
		t.Errorf("the refusal page sends Vary %q, which tells a cache nothing about what it varies on",
			got.Header().Get("Vary"))
	}
}

// TestARefusalPageWithNoCatalogKeepsTheKernelSentence. The other half of the added
// comment — "the English sentence below stays as the fallback for a shell with no
// catalog" — and it holds today, which is why it is written as a guard rather than a
// complaint: a shell that ships no catalog must keep saying the kernel's sentence in
// English and must keep declaring "en", because declaring Portuguese over English copy
// is the same lie in the other direction. A fix that translated the page and lost the
// fallback would be caught here.
func TestARefusalPageWithNoCatalogKeepsTheKernelSentence(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/tasks", nil)
	req.Header.Set("Accept-Language", "pt-PT, en;q=0.8")
	got := renderFault(t, shell(), csrfRefusal(), req)
	body := got.Body.String()
	if got.Code != http.StatusForbidden {
		t.Fatalf("the refusal = %d, want the guard's verdict: %s", got.Code, firstBytes(body))
	}
	if !strings.Contains(body, "came from another site") {
		t.Errorf("a shell with no catalog did not say the kernel's own sentence: %s", firstBytes(body))
	}
	if !strings.Contains(body, `lang="en"`) {
		t.Errorf("a page of English copy declared a language other than English, which is the mistake "+
			"the existing pin in language_test.go refuses: %s", firstBytes(body))
	}
}

func firstBytes(s string) string {
	if len(s) > 700 {
		return s[:700] + "…"
	}
	return s
}
