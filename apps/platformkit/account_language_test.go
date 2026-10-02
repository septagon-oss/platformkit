package main

// The person's language and the tenant's languages, asked of the running composition.
//
// Every case reaches its assertion through what the fixed behaviour prints — a 200,
// the page's declared lang, the catalogue's Portuguese title "Painel" — and never
// through the English a defect would print.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// languageApp boots the reference composition with acme served in English by
// default and in Portuguese besides, and answers its config and the administrator.
func languageApp(t *testing.T, declaration string) (config.Config, *http.Client) {
	t.Helper()
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	declareLocale(t, cfg, admin, installationTenantID(t, cfg, admin, "acme"), declaration)
	return cfg, admin
}

// dashboard finds the admin dashboard for a signed-in caller asking in ranking,
// and answers the page.
func dashboard(t *testing.T, cfg config.Config, client *http.Client, ranking string) string {
	t.Helper()
	for _, at := range []string{"/app/admin/dashboard", "/app/dashboard", "/app", "/app/"} {
		status, _, _, html := r7navigate(t, cfg, client, acmeHost, at, ranking)
		if status == http.StatusOK {
			return html
		}
	}
	status, _, _, html := r7navigate(t, cfg, client, acmeHost, "/app/admin/dashboard", "en")
	t.Fatalf("no admin dashboard answers 200 at %s (/app/admin/dashboard = %d: %s)", acmeHost, status, firstLineOf(html))
	return ""
}

func pageLang(html string) string {
	if m := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

// TestAChosenLanguageRendersTheAdminAndSurvivesSigningOutAndIn: the person's own
// choice beats the browser's English, and is still theirs after a new session.
func TestAChosenLanguageRendersTheAdminAndSurvivesSigningOutAndIn(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)

	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/user/users/language", `{"locale":"pt-PT"}`); code != http.StatusOK {
		t.Fatalf("choosing pt-PT = %d %s", code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/logout", ""); code >= 400 {
		t.Fatalf("sign out = %d %s", code, body)
	}
	again := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	html := dashboard(t, cfg, again, "en-GB,en;q=0.9")
	if got := pageLang(html); got != "pt-PT" {
		t.Fatalf("after signing in again the dashboard declares lang=%q, want the chosen pt-PT: %s", got, firstLineOf(html))
	}
	if !strings.Contains(html, "Painel") {
		t.Errorf("the chosen-language dashboard does not carry admin.dashboard.title in pt-PT")
	}
}
