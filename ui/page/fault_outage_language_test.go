package page_test

// The outage refusal, in both of the shell's languages.
//
// A 503 from a kernel guard carries no code and one sentence, and there are three of
// those sentences in the tree today: the authorizer that could not decide, the plan
// that could not be read, the host that could not be resolved (`kit/httpx/authorize.go`,
// `kit/httpx/tenant.go`). None of them is about the caller's request — which is the
// test `faultKey`'s comment states and this file measures: a verdict that passes it is
// answered in the language the deployment ships, and the person at a Portuguese tenant
// is not handed English precisely when the installation is down.
//
// The English half is asserted too, because it is the half the guard wrote: this
// catalogue ships no English file, so an English browser reads which subsystem is down
// and nobody re-words it.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/page"
)

// outages is every sentence a guard answers a 503 with, quoted from the file that
// writes it, so a guard re-wording its own outage is caught here rather than leaving
// a Portuguese sentence pointing at a sentence that no longer exists.
var outages = []struct{ name, detail string }{
	{"an authorizer that could not decide", "authorization is temporarily unavailable"},
	{"a plan that could not be read", "the plan could not be read right now"},
	{"a host that could not be resolved", "this host cannot be resolved right now"},
}

func TestAnOutageRefusalIsAnsweredInTheLanguageTheShellShips(t *testing.T) {
	s := shell()
	s.Messages = xtext.Load("en", page.Catalogue())

	for _, outage := range outages {
		t.Run(outage.name, func(t *testing.T) {
			ask := func(accepted string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/app/admin/tasks", nil)
				req.Header.Set("Accept-Language", accepted)
				return renderFault(t, s, &problem.Problem{
					Status: http.StatusServiceUnavailable, Detail: outage.detail,
					Instance: "urn:request:9999aaaa-1111-2222-3333-444455556666",
				}, req)
			}

			portuguese := ask("pt-PT")
			body := portuguese.Body.String()
			if portuguese.Code != http.StatusServiceUnavailable {
				t.Errorf("the outage page answered %d, not the verdict the guard refused with", portuguese.Code)
			}
			if !strings.Contains(body, "Este serviço está indisponível por um momento. Espere e tente de novo.") {
				t.Errorf("a Portuguese request at an outage was not answered with the sentence the catalogue carries: %s",
					firstLine(body))
			}
			// The kernel's sentence is gone from the page — which is the point of the
			// key, and the reason the key exists for the status and not the sentence.
			if strings.Contains(body, outage.detail) {
				t.Errorf("the outage page still says the kernel's English line over its Portuguese one: %s", firstLine(body))
			}
			for _, declares := range []string{`lang="pt-PT"`} {
				if !strings.Contains(body, declares) {
					t.Errorf("a page whose sentence is Portuguese declares no pt-PT: %s", firstLine(body))
				}
			}
			if portuguese.Header().Get("Content-Language") != "pt-PT" {
				t.Errorf("Content-Language = %q, want pt-PT", portuguese.Header().Get("Content-Language"))
			}
			if !strings.Contains(body, "9999aaaa-1111-2222-3333-444455556666") {
				t.Errorf("the translated outage lost the reference an operator reads a log by: %s", firstLine(body))
			}

			// The same shell, the same outage, an English browser: the guard's own
			// words, which say which half of the installation is down, stand.
			english := ask("en")
			if !strings.Contains(english.Body.String(), outage.detail) {
				t.Errorf("an English request was re-worded away from the sentence the guard wrote: %s",
					firstLine(english.Body.String()))
			}
			if !strings.Contains(english.Body.String(), `lang="en"`) {
				t.Errorf("English copy declared a language the shell negotiated instead: %s",
					firstLine(english.Body.String()))
			}
		})
	}
}
