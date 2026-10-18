package page_test

// Which languages a request may be answered in is the tenant's decision and not the
// catalog's. The catalog says what this deployment has copy for; the tenant says
// which of those its people are served in, and what answers when the browser asked
// for something it does not serve. Until this moved to the tenant, the second
// question had one answer for every customer of the installation at once.
//
// These cases go through the shell and the negotiation a page actually meets — a
// resolved tenant, a public surface, the request's own Accept-Language — because the
// thing at risk is not SelectLocale on its own but where the tenant's list enters
// the chain.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// speaksFor is one tenant resolution and one authorizer: the tenant of the case, at
// one host, allowed to do nothing, because the page these cases read is public and
// says only which language it was written in.
type speaksFor struct{ tenant tenancy.Tenant }

func (s speaksFor) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if host != "acme.localhost" {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (speaksFor) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

// servedIn starts the one page this file needs — a page that names the language it
// was rendered in and the one label the negotiation had to choose between — for a
// tenant carrying the languages a case gives it.
func servedIn(t *testing.T, languages *tenancy.Languages) func(accept string) string {
	t.Helper()
	_, conn := dbtest.Schema(t)
	who := speaksFor{tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme", Languages: languages}}
	kernel, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: "localhost", Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := shell()
	// The kernel's own sentences, and one pair of files for the label the page
	// prints. `screens.edit` is the key ui/resource raises for a row's edit
	// button, so the copy here is the copy that ships — only the test's own
	// English half, which ui/resource deliberately does not carry.
	s.Messages = xtext.Load("en", page.Catalogue(), xtext.Source{
		Name: "the case",
		FS: fstest.MapFS{
			"en.json":    &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Edit"}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Editar"}}`)},
		},
	})
	page.Serve(public(kernel), s, page.Route{ID: "language", Method: http.MethodGet, Path: "/_language"}, httpx.Public(),
		func(_ context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return page.View{Body: []g.Node{g.Text(r.Locale.Language + " " + r.Locale.Text("screens.edit", "Edit"))}}, nil
		})

	return func(accept string) string {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost/_language", nil)
		if accept != "" {
			req.Header.Set("Accept-Language", accept)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("the page answered %d: %s", rec.Code, rec.Body.String())
		}
		for _, want := range []string{"pt-PT Editar", "en Edit"} {
			if strings.Contains(rec.Body.String(), want) {
				return want
			}
		}
		t.Fatalf("the page said neither of the two things it could: %s", firstLine(rec.Body.String()))
		return ""
	}
}

// TestATenantChoosesWhichOfTheDeploymentsLanguagesItIsServedIn is the brief's
// tenancy rule, end to end: Accept-Language intersected with the tenant's own set,
// and the tenant's default when nothing in the request survives that intersection.
func TestATenantChoosesWhichOfTheDeploymentsLanguagesItIsServedIn(t *testing.T) {
	for _, c := range []struct {
		name     string
		served   *tenancy.Languages
		accept   string
		sentence string
	}{
		{
			name:     "a tenant served only in Portuguese",
			served:   &tenancy.Languages{Default: "pt-PT"},
			accept:   "en-GB,en;q=0.9",
			sentence: "pt-PT Editar",
		},
		{
			name:     "a tenant served in both, asked in the other one",
			served:   &tenancy.Languages{Default: "pt-PT", Others: []string{"en"}},
			accept:   "en",
			sentence: "en Edit",
		},
		{
			name:     "the same tenant asked in Portuguese",
			served:   &tenancy.Languages{Default: "pt-PT", Others: []string{"en"}},
			accept:   "pt-PT",
			sentence: "pt-PT Editar",
		},
		{
			// The reason the default is the tenant's and not the catalog's: a browser
			// that asked for German is not answered in the deployment's English
			// because that is what the fallback file is — it is answered in the
			// language this tenant chose.
			name:     "a language this tenant does not serve",
			served:   &tenancy.Languages{Default: "pt-PT"},
			accept:   "de-DE,de;q=0.9",
			sentence: "pt-PT Editar",
		},
		{
			// A region is not a foreign language: a phone that says pt-BR is served
			// the deployment's Portuguese.
			name:     "a region of a language this tenant serves",
			served:   &tenancy.Languages{Default: "pt-PT"},
			accept:   "pt-BR,pt;q=0.9",
			sentence: "pt-PT Editar",
		},
		{
			// Declaring nothing is not the same as serving one language: the browser's
			// own list decides, exactly as it did before a tenant had a say.
			name:     "a tenant that declared nothing",
			served:   nil,
			accept:   "en",
			sentence: "en Edit",
		},
		{
			name:     "a tenant that declared nothing, asked in Portuguese",
			served:   nil,
			accept:   "pt-PT",
			sentence: "pt-PT Editar",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := servedIn(t, c.served)(c.accept); got != c.sentence {
				t.Errorf("asking %q served %q, want %q", c.accept, got, c.sentence)
			}
		})
	}
}
