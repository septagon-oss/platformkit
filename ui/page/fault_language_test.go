package page_test

// The language a refusal page is written in, held to the shape the brief gives it:
// the kernel publishes the code a refusal travels in, this package holds the one
// table from a code to a catalog key, the page is negotiated from the request, and
// it says which language it is in — in the attribute and in the header. The
// reviewer's case in review_round2_language_test.go is the one that was failing
// when this change published the codes and stopped there; these are the cases for
// the rest of the mechanism: every refusal the kernel publishes, in both of the
// shell's languages, and the way a catalog has nothing to say, which must leave
// English copy declaring English.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/page"
)

// refusal is one verdict a guard answers with, in the two languages one shell ships
// for it. The English half is also what a shell with no catalog is shown, so the
// pair is the whole promise: the kernel's words stay the fallback, and wherever the
// shell has words of its own they are what the person reads.
type refusal struct {
	name, detail, english, portuguese string
	status                            int
	// key is the catalog key this refusal's sentence lives under, which is the
	// case itself rather than something the test reimplements: fault.go's table
	// for a published code, the status for the two refusals that carry none.
	key string
}

// refusals is every refusal the page can be shown: the nine codes kit/httpx
// publishes and has a row in fault.go's table for, and the three verdicts that
// carry no code because the router, the recoverer or chi wrote the sentence
// themselves. A code with no page is not in it — that is the condition F1 of the
// third review made a rule: the table carries exactly what a person is shown, so
// every row of it gets a case here, in both of the shell's languages.
var refusals = []refusal{
	{
		name: "a caller nobody recognised", status: http.StatusUnauthorized,
		detail:     httpx.CodeAnonymous + ": this address asks who is asking and the request said nothing",
		key:        "fault." + httpx.CodeAnonymous,
		english:    "Sign in before sending this.",
		portuguese: "Inicie sessão antes de enviar isto.",
	},
	{
		name: "a caller without the permission", status: http.StatusForbidden,
		detail:     httpx.CodeDenied + ": the caller may not write plans",
		key:        "fault." + httpx.CodeDenied,
		english:    "You may not do this.",
		portuguese: "Não pode fazer isto.",
	},
	{
		name: "a grant that belongs to the operator", status: http.StatusForbidden,
		detail:     httpx.CodeNotOperator + ": tenant:manage is the operator's, and this is not the operator's tenant",
		key:        "fault." + httpx.CodeNotOperator,
		english:    "This operation belongs to the operator, not to your tenant.",
		portuguese: "Esta operação pertence à instalação, não ao seu tenant.",
	},
	{
		name: "an operation that never said who may call it", status: http.StatusForbidden,
		detail:     httpx.CodeUndeclared + ": this operation declares no authorization",
		key:        "fault." + httpx.CodeUndeclared,
		english:    "This operation never said who may call it.",
		portuguese: "Esta operação nunca disse quem a pode chamar.",
	},
	{
		name: "tenant work at a host that names no tenant", status: http.StatusForbidden,
		detail:     httpx.CodeNoTenant + ": this operation is tenant work and the host resolved to none",
		key:        "fault." + httpx.CodeNoTenant,
		english:    "This work belongs to a tenant and this host names none.",
		portuguese: "Este trabalho pertence a um tenant e esta morada não nomeia nenhum.",
	},
	{
		name: "a write prepared by a different account", status: http.StatusForbidden,
		detail:     httpx.CodePrincipalChanged + ": the signed-in account differs from the one that prepared this request",
		key:        "fault." + httpx.CodePrincipalChanged,
		english:    "The signed-in account is not the one that prepared this.",
		portuguese: "A conta com sessão iniciada não é a que preparou este pedido.",
	},
	{
		name: "a write that came from another site", status: http.StatusForbidden,
		detail:     httpx.CodeCSRFOrigin + ": this request carries a session cookie and came from another site",
		key:        "fault." + httpx.CodeCSRFOrigin,
		english:    "This request came from another site, so nothing was written.",
		portuguese: "Este pedido veio de outro site, por isso nada foi escrito.",
	},
	{
		// The public surface's own refusal: the one a person meets with no session,
		// no tenant of their own and no account to be told anything to.
		name: "the public write limit", status: http.StatusTooManyRequests,
		detail:     httpx.CodeLimitExhausted + ": too many anonymous submissions from this address",
		key:        "fault." + httpx.CodeLimitExhausted,
		english:    "Too many submissions from this address. Wait and try again.",
		portuguese: "Demasiados envios a partir deste endereço. Espere e tente de novo.",
	},
	{
		// The public surface's promise, kept at the writer: the route broke it and
		// the answer is a 500 with the cookie withheld. Its code is in the table
		// because a person can be standing at the page when it happens.
		name: "a public route that minted a cookie", status: http.StatusInternalServerError,
		detail:     httpx.CodePublicSetsACookie + ": the public surface promises nobody is remembered, and this route broke the promise",
		key:        "fault." + httpx.CodePublicSetsACookie,
		english:    "This page promises nobody standing here is remembered.",
		portuguese: "Esta página promete que ninguém aqui é lembrado.",
	},
	{
		name: "an address nobody mounted", status: http.StatusNotFound,
		detail:     "nothing is served at this address",
		key:        "fault.404",
		english:    "There is nothing to see at this address.",
		portuguese: "Não há nada para ver nesta morada.",
	},
	{
		// The sentence is kit/httpx's own methodNotAllowed, about the address and
		// not about the caller's request, which is what puts it beside the 404.
		name: "a verb this address does not take", status: http.StatusMethodNotAllowed,
		detail:     "this address does not accept POST requests",
		key:        "fault.405",
		english:    "This address does not take this kind of request.",
		portuguese: "Este endereço não aceita este pedido.",
	},
	{
		// A 500 carries no detail on purpose, so this is the sentence the page
		// writes for itself — and it is copy of this package's own, which makes it
		// the one refusal line a shell can also translate.
		name: "a handler that broke", status: http.StatusInternalServerError,
		detail:     "",
		key:        "fault.500",
		english:    "Something went wrong at our end.",
		portuguese: "Qualquer coisa correu mal do nosso lado.",
	},
}

// twoLanguageRefusals builds the shell of a deployment that ships every sentence
// above in both of its languages.
func twoLanguageRefusals(t *testing.T) page.Shell {
	t.Helper()
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	for _, r := range refusals {
		if err := messages.SetString(language.English, r.key, r.english); err != nil {
			t.Fatal(err)
		}
		if err := messages.SetString(language.EuropeanPortuguese, r.key, r.portuguese); err != nil {
			t.Fatal(err)
		}
	}
	s := shell()
	s.Messages = page.FromCatalog(messages)
	return s
}

// TestRefusalPagesSpeakBothLanguages is the brief's case (§9 test 16) widened to
// the whole table: every refusal fault.go can answer a page for, in both of the
// shell's languages, and the page saying which one it is in the attribute and the
// header as well as the sentence. The verdict is asserted beside the translation,
// because a refusal that translated itself into a 200 is a refusal nobody sees —
// and so is the code, which is the half of "<CODE>: <sentence>" a person reads
// back to support and an operator matches a log line against.
func TestRefusalPagesSpeakBothLanguages(t *testing.T) {
	s := twoLanguageRefusals(t)
	for _, r := range refusals {
		for _, want := range []struct{ accepted, language, sentence string }{
			{"pt-PT, en;q=0.8", "pt-PT", r.portuguese},
			{"pt-PT", "pt-PT", r.portuguese},
			{"en", "en", r.english},
			// Nothing the shell speaks: the catalog's own fallback answers, in English.
			{"ja", "en", r.english},
		} {
			t.Run(r.name+" asking for "+want.accepted, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/plans", nil)
				req.Header.Set("Accept-Language", want.accepted)
				got := renderFault(t, s, &problem.Problem{
					Status: r.status, Detail: r.detail, Instance: "urn:request:1111aaaa-2222-3333-4444-555566667777",
				}, req)
				body := got.Body.String()

				if got.Code != r.status {
					t.Errorf("the refusal page answered %d while saying %q", got.Code, want.sentence)
				}
				if !strings.Contains(body, want.sentence) {
					t.Errorf("the page does not say %q; this shell ships that sentence for the refusal, in %s: %s",
						want.sentence, want.language, firstLine(body))
				}
				if other := r.portuguese; want.sentence != other && strings.Contains(body, other) {
					t.Errorf("an %s refusal carries %s copy as well", want.language, other)
				}
				if !strings.Contains(body, `lang="`+want.language+`"`) {
					t.Errorf("the document declares no %s: %s", want.language, firstLine(body))
				}
				if got.Header().Get("Content-Language") != want.language {
					t.Errorf("Content-Language = %q, want %q", got.Header().Get("Content-Language"), want.language)
				}
				if got.Header().Get("Vary") != "Accept-Language" {
					t.Errorf("Vary = %q, want the one thing this page varies on", got.Header().Get("Vary"))
				}
				// What a person in front of a refusal needs survives the translation:
				// the reference an operator reads a log by, and the one link out.
				if !strings.Contains(body, "1111aaaa-2222-3333-4444-555566667777") || !strings.Contains(body, `href="/admin"`) {
					t.Errorf("a translated refusal lost its reference or its way out: %s", firstLine(body))
				}
				// A refusal that carries a code keeps it, whatever language the
				// sentence arrived in: it is the one part of the line both halves of
				// this composition are written against.
				if code, _, named := strings.Cut(r.detail, ": "); named {
					if !strings.Contains(body, code+":") {
						t.Errorf("the translation dropped the code %q from the page: %s", code, firstLine(body))
					}
				}
			})
		}
	}
}

// TestOneShellAnswersTwoRefusalsInTwoLanguages. A translation mechanism that only
// works when the catalog is complete is a mechanism that reports the wrong language
// the moment it is not, which is the moment a deployment is mid-translation. So the
// case is one shell and two refusals: the sentence it has, in the language asked
// for, and the sentence it has not — the kernel's own English, declaring English,
// in the same negotiation.
func TestOneShellAnswersTwoRefusalsInTwoLanguages(t *testing.T) {
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	if err := messages.SetString(language.EuropeanPortuguese, "fault."+httpx.CodeDenied, "Não pode fazer isto."); err != nil {
		t.Fatal(err)
	}
	s := shell()
	s.Messages = page.FromCatalog(messages)

	ask := func(detail string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/plans", nil)
		req.Header.Set("Accept-Language", "pt-PT")
		return renderFault(t, s, &problem.Problem{
			Status: http.StatusForbidden, Detail: detail, Instance: "urn:request:abcd",
		}, req)
	}

	translated := ask(httpx.CodeDenied + ": the caller may not write plans").Body.String()
	if !strings.Contains(translated, "Não pode fazer isto.") || !strings.Contains(translated, `lang="pt-PT"`) {
		t.Errorf("the refusal this shell has a sentence for was not answered in it: %s", firstLine(translated))
	}

	// The other refusal is the one code the one table leaves out on purpose — its
	// sentence names an address, which is data the caller has to have — so the
	// kernel's sentence stands, and with it the language it is written in.
	untranslatedDetail := httpx.CodeWriteElsewhere + ": this address reads the plan and does not write it; " +
		"write it at /api/v1/ops/billing/plans"
	untranslated := ask(untranslatedDetail).Body.String()
	if !strings.Contains(untranslated, untranslatedDetail) {
		t.Errorf("the kernel's own sentence was replaced by nothing: %s", firstLine(untranslated))
	}
	if strings.Contains(untranslated, "Não pode fazer isto.") {
		t.Errorf("one refusal's Portuguese sentence was pasted onto another refusal: %s", firstLine(untranslated))
	}
	if !strings.Contains(untranslated, `lang="en"`) {
		t.Errorf("English copy declared a language the shell negotiated instead: %s", firstLine(untranslated))
	}
}

func firstLine(body string) string {
	if len(body) > 500 {
		return body[:500] + "…"
	}
	return body
}
