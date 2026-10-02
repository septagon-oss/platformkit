package main

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The pseudo-locale must reach pages beyond the dashboard and language editor:
// these headings come from distinct admin and generated-screen renderers.
func TestSecondaryAdminPagesUseThePseudoLocale(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	id := installationTenantID(t, cfg, admin, "acme")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id.String()+"/locale",
		`{"default":"en","supported":["en-XA","pt-PT"]}`)
	if code != http.StatusOK {
		t.Fatalf("declare en-XA = %d: %s", code, body)
	}
	heading := regexp.MustCompile(`(?s)<h1\b[^>]*>(.*?)</h1>`)
	tags := regexp.MustCompile(`<[^>]*>`)
	plainWord := regexp.MustCompile(`\b[A-Za-z]{3,}\b`)
	for _, path := range []string{
		"/app/admin/health", "/app/auth/roles", "/app/site/settings",
		"/app/content/contents", "/app/billing/plans", "/app/tenant/tenants",
		"/app/admin/_gallery",
	} {
		status, _, contentLanguage, page := r7navigate(t, cfg, admin, acmeHost, path, "en-XA")
		if status != http.StatusOK || pageLang(page) != "en-XA" || contentLanguage != "en-XA" {
			t.Errorf("%s answered %d, html lang=%q, Content-Language=%q; want an en-XA page",
				path, status, pageLang(page), contentLanguage)
			continue
		}
		match := heading.FindStringSubmatch(page)
		if len(match) < 2 {
			t.Errorf("%s has no page heading", path)
			continue
		}
		visible := html.UnescapeString(tags.ReplaceAllString(match[1], ""))
		if plainWord.MatchString(visible) {
			t.Errorf("%s heading %q has unaccented source copy", path, visible)
		}
		if path == "/app/auth/roles" {
			if count := regexp.MustCompile(`\b[0-9]+ roles?\.`).FindString(page); count != "" {
				t.Errorf("%s retains the English count %q", path, count)
			}
			for _, copy := range []string{
				"New role", "Create the role", "A lower-case identifier",
				"everything in this tenant",
			} {
				if strings.Contains(page, copy) {
					t.Errorf("%s retains English interface copy %q", path, copy)
				}
			}
		}
	}
}

func TestHandWrittenAdminPagesSpeakPortuguese(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	heading := regexp.MustCompile(`(?s)<h1\b[^>]*>(.*?)</h1>`)
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, tc := range []struct{ path, english string }{
		{"/app/auth/roles", "Roles"},
		{"/app/tenant/tenants", "Tenants"},
		{"/app/admin/_gallery", "Components"},
	} {
		status, _, contentLanguage, page := r7navigate(t, cfg, admin, acmeHost, tc.path, "pt-PT")
		if status != http.StatusOK || pageLang(page) != "pt-PT" || contentLanguage != "pt-PT" {
			t.Errorf("%s answered %d, html lang=%q, Content-Language=%q; want a pt-PT page",
				tc.path, status, pageLang(page), contentLanguage)
			continue
		}
		match := heading.FindStringSubmatch(page)
		if len(match) < 2 {
			t.Errorf("%s has no page heading", tc.path)
			continue
		}
		visible := html.UnescapeString(tags.ReplaceAllString(match[1], ""))
		if visible == tc.english {
			t.Errorf("%s heading is still English %q on a pt-PT page", tc.path, visible)
		}
	}
}
