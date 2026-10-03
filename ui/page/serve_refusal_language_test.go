package page_test

// A refusal a module makes is answered in the tenant's own language.
//
// `Serve` turned a handler's own 4xx into `page.Fault(status, detail, back, backLabel)`
// — the exported entry point, which takes no locale — while `FaultHandler` used the
// unexported `fault(…, loc, …)`, which does. The consequence was that the refusal a
// module makes was English *in the shell that ships the sentence for it*, and the
// reproduction of that refusal had to reach a kernel guard to be answered in Portuguese.
// Both kinds of refusal now go through one function, so this file asks both halves of
// that and nothing else:
//
//   - a shell that ships the catalogues answers a handler's 404 with its own sentence,
//     in the language the request brought, with the two headers every page of this
//     shell carries and the way out the shell offers;
//   - a shell that ships none keeps the handler's own sentence, keeps declaring "en",
//     and sends no `Content-Language` — the branch `fault.go` documents, which a cure
//     that claimed the negotiated language over English copy would have broken.
//
// The handler here refuses with the sentence `modules/web` writes for a slug nobody
// published, so the second half asserts the thing the first half trades away: once a
// shell ships `fault.404`, a module's own sentence about its own address gives way to
// the shell's generic one, in the language the request brought. That is what the same
// key already does to a kernel 404, and it is the cost of answering a person in their
// own language from a catalogue rather than from a string; the named slug is what a
// writer keeps until a refusal can carry a key of its own, which is the kernel's share
// and is named as such in this task's report.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// refusedAt mounts the one page this file needs — a page whose handler refuses every
// request the way a module's page handler does — in a shell that either ships the
// deployment's refusal catalogue or ships none.
func refusedAt(t *testing.T, shipped bool) func(accept string) *httptest.ResponseRecorder {
	t.Helper()
	_, conn := dbtest.Schema(t)
	who := speaksFor{tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme",
		Languages: &tenancy.Languages{Default: "en", Others: []string{"pt-PT"}}}}
	kernel, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: "localhost", Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := shell()
	if shipped {
		s.Messages = xtext.Load("en", page.Catalogue())
	}
	page.Serve(public(kernel), s, page.Route{ID: "gone", Method: http.MethodGet, Path: "/_gone"}, httpx.Public(),
		func(_ context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return page.View{}, problem.NotFound("there is no page at /_gone")
		})

	return func(accept string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost/_gone", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Accept-Language", accept)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("the handler's own refusal answered %d: %s", rec.Code, firstLine(rec.Body.String()))
		}
		return rec
	}
}

// TestARefusalAModuleMakesIsAnsweredInTheLanguageTheShellSpeaks.
func TestARefusalAModuleMakesIsAnsweredInTheLanguageTheShellSpeaks(t *testing.T) {
	t.Parallel()
	asked := refusedAt(t, true)

	portuguese := asked("pt-PT,pt;q=0.9")
	for _, want := range []string{
		"Não há nada para ver nesta morada.", // the sentence this package ships for a 404
		`lang="pt-PT"`,                       // and says which language it is in
		`href="/admin"`,                      // the shell's own way out
	} {
		if !strings.Contains(portuguese.Body.String(), want) {
			t.Errorf("a module's 404 in a shell that ships the catalogues omits %q: %s",
				want, firstLine(portuguese.Body.String()))
		}
	}
	if got := portuguese.Header().Get("Content-Language"); got != "pt-PT" {
		t.Errorf("Content-Language = %q, want the language on the page", got)
	}
	if got := portuguese.Header().Get("Vary"); got != "Accept-Language" {
		t.Errorf("Vary = %q, want the one thing this page varies on", got)
	}

	// The same shell, asked in English: no entry there, so the handler's own
	// sentence stands and the page says so rather than the negotiation.
	english := asked("en")
	if body := english.Body.String(); !strings.Contains(body, "there is no page at /_gone") {
		t.Errorf("the source language lost the handler's own sentence: %s", firstLine(body))
	}
	if got := english.Header().Get("Content-Language"); got != "en" {
		t.Errorf("Content-Language = %q, want the source language the page speaks", got)
	}
}

// TestARefusalInAShellThatShipsNoCatalogueKeepsItsOwnSentence is the other half: what
// `ui/page/fault.go` promises a shell with no catalog, at the entry point that was
// written before that promise existed.
func TestARefusalInAShellThatShipsNoCatalogueKeepsItsOwnSentence(t *testing.T) {
	t.Parallel()
	asked := refusedAt(t, false)

	portuguese := asked("pt-PT,pt;q=0.9")
	body := portuguese.Body.String()
	if !strings.Contains(body, "there is no page at /_gone") {
		t.Errorf("a shell with no catalogue replaced the handler's sentence with nothing: %s", firstLine(body))
	}
	if strings.Contains(body, "Não há nada para ver nesta morada.") {
		t.Errorf("a shell with no catalogue answered in a language it has no copy in: %s", firstLine(body))
	}
	if !strings.Contains(body, `lang="en"`) {
		t.Errorf("English copy declared a language the shell cannot speak: %s", firstLine(body))
	}
	if got := portuguese.Header().Get("Content-Language"); got != "" {
		t.Errorf("Content-Language = %q from a shell that negotiated nothing", got)
	}
}
