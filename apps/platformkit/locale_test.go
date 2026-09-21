package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func TestReferenceSignInUsesIsolatedNegotiatedTranslations(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	c.modules = append(c.modules, module.Module{Name: "locale_verification", Routes: func(r httpx.Surfaces) {
		shell := page.Shell{Messages: page.FromCatalog(admin.Messages()), Frame: func(_ context.Context, r page.Request, body []g.Node) g.Node {
			return h.Main(h.Lang(r.Locale.Language), g.Group(body))
		}}
		ownedShell := shell
		ownedShell.Messages = independentMessages{}
		page.Serve(r.App, ownedShell, page.Route{ID: "locale-provider", Method: http.MethodGet, Path: "/_locale/provider"}, httpx.Public(),
			func(_ context.Context, r page.Request, _ *page.Empty) (page.View, error) {
				return page.View{Body: []g.Node{g.Text(r.Locale.Text("welcome", "Welcome, %s", "Camille"))}}, nil
			})
		page.Serve(r.App, shell, page.Route{ID: "locale-authored", Method: http.MethodGet, Path: "/_locale/english"}, httpx.Public(),
			func(context.Context, page.Request, *page.Empty) (page.View, error) {
				return page.View{Language: "en", Body: []g.Node{g.Text("Authored English")}}, nil
			})
		page.Serve(r.App, shell, page.Route{ID: "locale-refusal", Method: http.MethodGet, Path: "/_locale/refusal"}, httpx.Public(),
			func(context.Context, page.Request, *page.Empty) (page.View, error) {
				return page.View{}, problem.New(http.StatusForbidden, "English refusal")
			})
	}})
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	for _, tc := range []struct {
		path, accepted, language, text string
		status                         int
	}{
		{"/app/admin/login", "pt-PT, en;q=0.8", "pt-PT", "Palavra-passe", 200},
		{"/app/admin/login?lang=en", "pt-PT", "en", "Password", 200},
		{"/app/admin/login?lang=pt-PT", "en", "pt-PT", "Palavra-passe", 200},
		{"/app/admin/login?lang=ja", "pt-PT", "pt-PT", "Palavra-passe", 200},
		{"/app/admin/login?lang=en&lang=pt-PT", "en", "en", "Password", 200},
		{"/app/admin/login", "ja", "en", "Password", 200},
		{"/app/admin/login", "", "en", "Password", 200},
		{"/app/locale_verification/_locale/provider", "en", "fr", "Bonjour, Camille", 200},
		{"/app/locale_verification/_locale/english", "pt-PT", "en", "Authored English", 200},
		{"/app/locale_verification/_locale/refusal", "pt-PT", "en", "English refusal", 403},
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = acmeHost
		req.Header.Set("Accept-Language", tc.accepted)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		if response.StatusCode != tc.status || !strings.Contains(body, `lang="`+tc.language+`"`) || !strings.Contains(body, tc.text) {
			t.Fatalf("page %s accepting %q = %d, expected %s copy", tc.path, tc.accepted, response.StatusCode, tc.language)
		}
		if response.Header.Get("Content-Language") != tc.language || response.Header.Get("Vary") != "Accept-Language" || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("negotiated headers: language=%q vary=%q cache=%q", response.Header.Get("Content-Language"), response.Header.Get("Vary"), response.Header.Get("Cache-Control"))
		}
		if strings.HasPrefix(tc.path, "/app/locale_verification/_locale/") {
			if !strings.Contains(body, `<main lang="`+tc.language+`">`) {
				t.Fatal("frame used a different language than the selected provider or view")
			}
			continue
		}
		for _, contract := range []string{`action="` + pinnedSignInAPI + `"`, `name="email"`, `name="password"`, `data-login-form`} {
			if !strings.Contains(body, contract) {
				t.Fatalf("localized form lost %s", contract)
			}
		}
	}
}

// A second provider proves page consumers need no x/text catalog or printer.
type independentMessages struct{}

func (independentMessages) Select(...string) page.Locale {
	return page.Locale{Language: "fr", Formatter: independentMessages{}}
}

func (independentMessages) Text(key, fallback string, args ...any) string {
	if key == "welcome" {
		return fmt.Sprintf("Bonjour, %s", args...)
	}
	return fmt.Sprintf(fallback, args...)
}
