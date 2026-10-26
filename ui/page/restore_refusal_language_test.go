package page_test

import (
	"context"
	"html"
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

// A restore refused for a version the record never held reaches a browser with the
// heading and sentence the deployment ships for VERSION_UNUSABLE in each of its two
// languages, never the bare status text; the English page also keeps the guard's own
// line, the one place the number that was asked for is written.
func TestARestoreRefusalIsWordedInEachLanguageTheDeploymentShips(t *testing.T) {
	t.Parallel()
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
	s.Messages = xtext.Load("en", page.Catalogue())
	// The refusal kit/rest's restore door writes, byte for byte.
	page.Serve(public(kernel), s, page.Route{ID: "restore", Method: http.MethodGet, Path: "/_restore"}, httpx.Public(),
		func(context.Context, page.Request, *page.Empty) (page.View, error) {
			return page.View{}, problem.New(http.StatusUnprocessableEntity,
				httpx.CodeVersionUnusable+": a version is numbered from 1, and 0 is not one")
		})

	for _, c := range []struct{ accept, language, heading, sentence string }{
		{"en", "en", "Version unavailable",
			"A version is numbered from 1, and the number asked for is not one of this record's versions."},
		{"pt-PT,pt;q=0.9", "pt-PT", "Versão indisponível",
			"Uma versão é numerada a partir de 1, e o número pedido não é uma versão deste registo."},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://acme.localhost/_restore", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Accept-Language", c.accept)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusUnprocessableEntity || rec.Header().Get("Content-Language") != c.language {
			t.Errorf("Accept-Language %q answered %d in %q: %s", c.accept, rec.Code,
				rec.Header().Get("Content-Language"), firstLine(body))
			continue
		}
		for _, want := range []string{c.heading, c.sentence} {
			if !strings.Contains(body, html.EscapeString(want)) {
				t.Errorf("the %s restore refusal omits %q: %s", c.language, want, firstLine(body))
			}
		}
		if strings.Contains(body, http.StatusText(http.StatusUnprocessableEntity)) {
			t.Errorf("the %s restore refusal is headed by the status text", c.language)
		}
		if guard := strings.Contains(body, "0 is not one"); guard != (c.language == "en") {
			t.Errorf("the %s restore refusal shows the guard's English line = %v", c.language, guard)
		}
	}
}
