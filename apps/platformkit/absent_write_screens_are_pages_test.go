package main

// A generated resource that declares no create or no update verb mounts no form for it, and the
// reference shell answers the form's address as its own 404 page — not the 403 a grant refusal
// gives, and not the problem document a record route would write if it took "new" for an id.
// The case drives the composed application, so a resource some module declares read-only is
// answered the way kit/rest's own fixtures are, in whichever language the tenant declared.
//
// Reachability runs through the fixed behaviour: the administrator is served the read-only list
// and a writable resource's New form first, so a 404 below is an absent form, not a missing grant.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAReadOnlyResourceAnswersItsAbsentFormsWithAPageInTheTenantsLanguage asks the webhook
// module's delivery log (list and read only) for both forms and its endpoint list (no create)
// for New, once with the tenant declared in English and once in Portuguese.
func TestAReadOnlyResourceAnswersItsAbsentFormsWithAPageInTheTenantsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acme := installationTenantID(t, cfg, admin, "acme")
	langOf := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`)
	absent := []string{
		"/app/webhook/deliveries/new",
		"/app/webhook/deliveries/" + uuid.NewString() + "/edit",
		"/app/webhook/endpoints/new",
	}

	for _, lang := range []string{"en", "pt-PT"} {
		declareLocale(t, cfg, admin, acme, `{"default":"`+lang+`","supported":[]}`)

		for _, control := range []string{"/app/webhook/deliveries", "/app/task/tasks/new"} {
			status, ctype, _, html := r7navigate(t, cfg, admin, acmeHost, control, "en-GB,en;q=0.9")
			if status != http.StatusOK || !strings.HasPrefix(ctype, "text/html") {
				t.Fatalf("[%s] GET %s for the administrator = %d %q, want the screen: %s",
					lang, control, status, ctype, firstLineOf(html))
			}
		}

		for _, at := range absent {
			status, ctype, _, html := r7navigate(t, cfg, admin, acmeHost, at, "en-GB,en;q=0.9")
			if status != http.StatusNotFound {
				t.Errorf("[%s] GET %s = %d, want 404: the resource declares no such form: %s",
					lang, at, status, firstLineOf(html))
				continue
			}
			if !strings.HasPrefix(ctype, "text/html") ||
				!strings.HasPrefix(strings.ToLower(strings.TrimSpace(html)), "<!doctype html") {
				t.Errorf("[%s] GET %s answered %q, want the shell's 404 page: %s", lang, at, ctype, firstLineOf(html))
				continue
			}
			if m := langOf.FindStringSubmatch(html); m == nil || m[1] != lang {
				t.Errorf("[%s] the 404 page at %s declares %v, want lang=%q: %s", lang, at, m, lang, firstLineOf(html))
			}
		}
	}
}
