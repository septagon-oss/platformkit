package main

// A reviewer's pin, T-0111 review round 1. Nothing here fails on this branch; it pins the
// two assertions this delivery's Portuguese rests on and nothing else in the tree holds
// together — that a page's `<html lang>` and its Content-Language header say one thing,
// and that a tenant which never declared a language is nonetheless answered in the
// deployment's second language by a browser that offered it. The second is measured and
// printed, not asserted, because what it should say is the finding; the first is asserted,
// because the two places that spell the language out are owned by different files
// (ui/page.Document and ui/page.Serve) and either could move without the other.

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// declaredLanguage is the language of one page, both ways it is said.
func declaredLanguage(t *testing.T, cfg config.Config, client *http.Client, path, accept string) (int, string, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept-Language", accept)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	lang := ""
	if m := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(string(body)); m != nil {
		lang = m[1]
	}
	return res.StatusCode, lang, res.Header.Get("Content-Language"), string(body)
}

func TestEveryPageSaysTheSameLanguageTwice(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// acme is the fixture tenant: its operator never called the locale route, so the
	// languages below are the ones the composition gave it, not the ones anybody chose.
	for _, c := range []struct{ path, accept string }{
		{"/app/admin/login", "pt-PT,pt;q=0.9"},
		{"/app", "pt-PT,pt;q=0.9"},
		{"/app/task/tasks", "pt-PT,pt;q=0.9"},
		{"/app/task/tasks", "en"},
		{"/app/task/tasks", ""},
	} {
		code, lang, header, body := declaredLanguage(t, cfg, admin, c.path, c.accept)
		if code != http.StatusOK {
			t.Fatalf("%s answered %d", c.path, code)
		}
		if lang == "" || header == "" {
			t.Errorf("%s said no language twice: <html lang=%q, Content-Language %q", c.path, lang, header)
			continue
		}
		if lang != header {
			t.Errorf("%s declares lang=%q in the document and Content-Language %q in the header: one page, "+
				"two answers about the language it is in", c.path, lang, header)
		}
		// Measured, not asserted: what the tenant that never spoke was answered in.
		t.Logf("%-18s accept=%-16q -> lang=%-6s Portuguese-copy=%v English-copy=%v", c.path, c.accept, lang,
			strings.Contains(body, "Iniciar sessão") || strings.Contains(body, "Eliminar") || strings.Contains(body, "Ainda sem"),
			strings.Contains(body, "Dashboard") || strings.Contains(body, "Delete"))
	}
}
