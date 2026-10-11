package main

// A reviewer's case, T-0111 review round 6 (2026-09-30).
//
// Round 5's HIGH — the refusal catalogue never read at the application — is cured and
// I re-measured it at a live server (POST /app and a handler's 404 both answer
// `Content-Language: pt-PT` for the tenant bootstrapped `--language pt-PT`). What no
// case in this tree asks is the pillar contract's own question about that now-live
// path, line 1: "Name the test that fails when a second tenant's … **locale** … is
// reachable from the first."
//
// `review_r3_two_tenants_two_languages_test.go` asks it of a page — the sign-in page,
// through `page.Serve`, where the shell has a resolved tenant, an open transaction and
// a preference resolver. A refusal answered by a kernel guard has none of those:
// `ui/page.faultLocale` (refusalLocale) negotiates from `read(ctx, s.Chrome).Tenant`,
// which is whatever the chain managed to resolve before it refused, and its own
// comment accepts that "a guard that refused before a host resolved has no tenant to
// filter by". The case below turns that acceptance into a measurement by putting two
// tenants with opposite declarations behind one process and sending one header at both
// hosts — the header naming the language the *neighbour* declared, so an answer that
// ignored the tenant is indistinguishable from one that crossed over.
//
// Both directions are asserted, and each has its own passing branch:
//
//   - a tenant served in English alone, asked in Portuguese, is refused in English and
//     carries none of its neighbour's copy;
//   - a tenant served in Portuguese alone, asked in English, is refused in the language
//     it actually declared — which is the tenant's declaration overriding a browser, the
//     same rule `?lang=en` obeys on the page (`review_r3`'s last block).
//
// Every assertion reaches the refusal through what the refusal prints whatever its
// language — its status and its media type — and then asks which language the document
// claims and which tenant's sentence is on it. It never asks for the English line the
// defect would print.

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

// refusedAsABrowser asks one address the way a navigating client does — an explicit
// `text/html`, which is what makes the kernel answer a refusal as a page at all — at a
// chosen host, and answers the status, the media type, `Content-Language` and the body.
func refusedAsABrowser(t *testing.T, cfg config.Config, method, host, path, accept string) (int, string, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", accept)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s %s at %s: %v", method, path, host, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Language"), string(body)
}

// declaredLang is the language the document says its own words are in.
func declaredLang(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page declared no language at all: %s", firstLineOf(body))
	}
	return m[1]
}

// TestTwoTenantsAreRefusedInTheirOwnLanguages — the refusal path, at both hosts.
func TestTwoTenantsAreRefusedInTheirOwnLanguages(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexUUID := installationTenantID(t, cfg, admin, "globex")
	acmeUUID := installationTenantID(t, cfg, admin, "acme")

	// Two declarations that disagree: acme serves Portuguese alone, globex English
	// alone. Written by the same operator, at the same route, in the same process.
	declareLocale(t, cfg, admin, acmeUUID, `{"default":"pt-PT","supported":[]}`)
	declareLocale(t, cfg, admin, globexUUID, `{"default":"en","supported":[]}`)

	// Reachability, and it is a control: the same header, at both hosts, on the one
	// page a person with no session always can read. If acme is Portuguese and globex
	// is English here, the tenant boundary holds on a page and the two cases below are
	// asking whether it holds on a refusal. If it fails, nothing below means anything.
	const header = "pt-PT,pt;q=0.9"
	for _, want := range []struct {
		host, declared, ownCopy string
	}{
		{acmeHost, `lang="pt-PT"`, "Iniciar sessão"},
		{globexHost, `lang="en"`, "Sign in"},
	} {
		status, html := getLanguage(t, cfg, want.host, "/app/admin/login", header)
		if status != http.StatusOK {
			t.Fatalf("GET /app/admin/login at %s = %d: %s", want.host, status, firstLineOf(html))
		}
		if !strings.Contains(html, want.declared) || !strings.Contains(html, want.ownCopy) {
			t.Fatalf("the fixture's own page at %s is not served in %s with %q, so the two cases below prove "+
				"nothing: %s", want.host, want.declared, want.ownCopy, firstLineOf(html))
		}
	}

	// A guard refuses ahead of routing (405) and a handler refuses its own page (404).
	// Both at both hosts, with the same header naming the neighbour's language.
	for _, refusal := range []struct {
		name, method, path string
		status             int
	}{
		{"a guard refuses ahead of routing", http.MethodPost, "/app", http.StatusMethodNotAllowed},
		{"a handler refuses its own page", http.MethodGet, "/_no_reviewer_here_", http.StatusNotFound},
	} {
		t.Run(refusal.name+" at the tenant served in English alone", func(t *testing.T) {
			status, contentType, header_, body := refusedAsABrowser(t, cfg, refusal.method, globexHost, refusal.path, header)
			if status != refusal.status {
				t.Fatalf("%s %s at %s = %d: %s", refusal.method, refusal.path, globexHost, status, firstLineOf(body))
			}
			if !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("a navigating caller was answered %s, so no page was rendered at all", contentType)
			}
			// The fixed behaviour, and the only thing that makes this case mean what it
			// says: a tenant that declared English alone is refused in English.
			if header_ != "en" {
				t.Errorf("a refusal rendered at %s (which declared English alone) carries Content-Language %q: "+
					"the language a tenant is served in is its declaration, and the guard answered the browser "+
					"instead of the tenant", globexHost, header_)
			}
			if got := declaredLang(t, body); got != "en" {
				t.Errorf("the refusal page at %s declares lang=%q, a language this tenant never declared: %s",
					globexHost, got, firstLineOf(body))
			}
			for _, neighbour := range []string{"Não há nada", "Este endereço", "Iniciar sessão"} {
				if strings.Contains(body, neighbour) {
					t.Errorf("the refusal at %s carries %q, the copy of the neighbour that declared Portuguese alone: %s",
						globexHost, neighbour, firstLineOf(body))
				}
			}
		})

		t.Run(refusal.name+" at the tenant served in Portuguese alone", func(t *testing.T) {
			status, contentType, header_, body := refusedAsABrowser(t, cfg, refusal.method, acmeHost, refusal.path, header)
			if status != refusal.status {
				t.Fatalf("%s %s at %s = %d: %s", refusal.method, refusal.path, acmeHost, status, firstLineOf(body))
			}
			if !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("a navigating caller was answered %s, so no page was rendered at all", contentType)
			}
			if header_ != "pt-PT" {
				t.Errorf("a refusal at %s (which declared Portuguese alone) carries Content-Language %q", acmeHost, header_)
			}
			if got := declaredLang(t, body); got != "pt-PT" {
				t.Errorf("the refusal page at %s declares lang=%q: %s", acmeHost, got, firstLineOf(body))
			}
		})
	}

	// The browser naming the *neighbour's* language at the tenant that refused it:
	// acme declares pt-PT alone, so an English-only header must not move it. This is
	// the same rule `?lang=en` obeys on a page, asked of a refusal.
	t.Run("a browser naming the neighbour's language does not move the refusal", func(t *testing.T) {
		for _, refusal := range []struct {
			method, path string
			status       int
		}{
			{http.MethodPost, "/app", http.StatusMethodNotAllowed},
			{http.MethodGet, "/_no_reviewer_here_", http.StatusNotFound},
		} {
			status, contentType, header_, body := refusedAsABrowser(t, cfg, refusal.method, acmeHost, refusal.path, "en-GB,en;q=0.9")
			if status != refusal.status {
				t.Fatalf("%s %s at %s = %d: %s", refusal.method, refusal.path, acmeHost, status, firstLineOf(body))
			}
			if !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("a navigating caller was answered %s, so no page was rendered at all", contentType)
			}
			if header_ != "pt-PT" || declaredLang(t, body) != "pt-PT" {
				t.Errorf("acme is served in Portuguese alone and its refusal answered Content-Language %q with "+
					"lang=%q: the tenant's set binds a page and must bind a refusal reached through a guard: %s",
					header_, declaredLang(t, body), firstLineOf(body))
			}
		}
	})
}
