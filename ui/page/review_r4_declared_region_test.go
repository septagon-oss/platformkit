package page_test

// A reviewer's pin, T-0111 review round 4 (2026-09-29).
//
// The invariant this brief states is "negotiation is Accept-Language ∩ the tenant's
// locales, default the tenant's". Every case the branch shipped exercises a
// deployment that carries ONE file per language — en.json and pt-PT.json — so the
// intersection and the deployment's copy are the same set and the assertion cannot
// tell "restricted to the tenant" apart from "restricted to the files".
//
// A deployment that carries two regions of one language can tell them apart, and
// xtext.Load invites one: a source directory is `<locale>.json` files and any
// composition may ship pt-PT.json beside pt-BR.json (sickermule.com, the client this
// brief names as the reason it exists, serves more than one Portuguese). This file
// composes that deployment — three files, three tags — and asks whether the language
// a tenant is answered in is one it declared. It asserts the answer is a member of
// the tenant's own set, read out of the `lang` attribute the page prints (which both
// the broken and the fixed behaviour write, so the case reaches its assertion
// whatever the answer is), and it asserts the person's own region still wins when
// the tenant declared it, so the cure cannot be "always take the default".

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// threeRegions is the tenant resolution of these cases: one tenant, one host, no
// grants, because the page under test is public and says only what language it is in.
type threeRegions struct{ tenant tenancy.Tenant }

func (s threeRegions) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if host != "acme.localhost" {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (threeRegions) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

// htmlLang is the language the document declares about itself.
var htmlLang = regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`)

// servedFrom starts the one page these cases read for a tenant carrying the given
// languages, over a deployment that carries three: the source language and two
// regions of Portuguese. It answers with the language the response declared and the
// label the negotiation chose, both read out of the body, and the Content-Language
// header.
func servedFrom(t *testing.T, languages *tenancy.Languages) func(accept string) (lang, label, header string) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	who := threeRegions{tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme", Languages: languages}}
	kernel, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: "localhost", Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := shell()
	// One key, spelled in all three files, because parity per source is the
	// loader's own rule and a fixture that broke it would fail at composition
	// rather than at the negotiation under test. `screens.edit` is the key
	// ui/resource raises for a row's edit button; the copy is the copy that ships.
	s.Messages = xtext.Load("en", page.Catalogue(), xtext.Source{
		Name: "the case",
		FS: fstest.MapFS{
			"en.json":    &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Edit"}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Editar"}}`)},
			"pt-BR.json": &fstest.MapFile{Data: []byte(`{"screens.edit": {"translation": "Editar (Brasil)"}}`)},
		},
	})
	page.Serve(public(kernel), s, page.Route{ID: "language", Method: http.MethodGet, Path: "/_language"}, httpx.Public(),
		func(_ context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return page.View{Body: []g.Node{g.Text(r.Locale.Text("screens.edit", "Edit"))}}, nil
		})

	return func(accept string) (string, string, string) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost/_language", nil)
		if accept != "" {
			req.Header.Set("Accept-Language", accept)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("the page answered %d: %s", rec.Code, firstLine(rec.Body.String()))
		}
		body := rec.Body.String()
		m := htmlLang.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("the page declared no language: %s", firstLine(body))
		}
		label := ""
		for _, candidate := range []string{"Edit", "Editar", "Editar (Brasil)"} {
			if strings.Contains(body, candidate) {
				label = candidate
			}
		}
		return m[1], label, rec.Header().Get("Content-Language")
	}
}

// TestTheLanguageAPersonIsAnsweredInIsOneTheTenantDeclared is the brief's invariant
// stated where it can actually be falsified: the answer must be a member of the
// tenant's own set, not merely a language the deployment happens to have a file for.
func TestTheLanguageAPersonIsAnsweredInIsOneTheTenantDeclared(t *testing.T) {
	for _, c := range []struct {
		name   string
		served *tenancy.Languages
		accept string
		want   string
	}{
		{
			// The case the branch never composed: the deployment carries pt-BR and
			// the tenant never declared it. "∩ the tenant's locales" says the
			// person is answered in the tenant's Portuguese; the filter as written
			// compares only the language half and hands the whole tag to the
			// matcher, which has a pt-BR file to match it against.
			name:   "a tenant served pt-PT, asked in a region it does not serve",
			served: &tenancy.Languages{Default: "pt-PT"},
			accept: "pt-BR,pt;q=0.9",
			want:   "pt-PT",
		},
		{
			// The mirror image, so the case is about the tenant's set and not about
			// one region winning in general.
			name:   "a tenant served pt-BR, asked in a region it does not serve",
			served: &tenancy.Languages{Default: "pt-BR"},
			accept: "pt-PT,pt;q=0.9",
			want:   "pt-BR",
		},
		{
			// The person's region wins when the tenant serves it. The cure for the
			// two above is not "ignore the region the browser named": a tenant
			// that declared both regions declared both.
			name:   "a tenant that serves the region the person named",
			served: &tenancy.Languages{Default: "pt-PT", Others: []string{"pt-BR"}},
			accept: "pt-BR,pt;q=0.9",
			want:   "pt-BR",
		},
		{
			// And the same with the ranking turned round: the person who asked for
			// Portuguese first and European Portuguese second is served the tenant's
			// Portuguese either way, and the tenant's default is what answers.
			name:   "a tenant served pt-PT, asked in bare Portuguese",
			served: &tenancy.Languages{Default: "pt-PT"},
			accept: "pt;q=0.9,pt-BR;q=0.8",
			want:   "pt-PT",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			lang, label, header := servedFrom(t, c.served)(c.accept)
			// The label is what proves the language attribute is not a decoration:
			// the copy on the page is the copy of the language the page claims.
			if label == "" {
				t.Fatalf("the page carried none of the copy this deployment ships")
			}
			if lang != c.want {
				t.Errorf("a tenant serving %v answered %q in %q (%q): the language a person is "+
					"served in is the tenant's declaration, and ∩ its set is empty for anything else",
					declared(c.served), c.accept, lang, label)
			}
			if header != "" && header != lang {
				t.Errorf("the document declares lang=%q and Content-Language %q: one response, two languages",
					lang, header)
			}
		})
	}
}

// declared names the tenant's set in a failure message.
func declared(l *tenancy.Languages) []string {
	if l == nil {
		return nil
	}
	return append([]string{l.Default}, l.Others...)
}
