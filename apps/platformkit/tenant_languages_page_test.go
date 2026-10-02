package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestAnAdministratorReachesTheTenantsLanguagesFromTheAdmin: Settings → Languages is
// a page a tenant administrator can open from the admin and answers 200.
func TestAnAdministratorReachesTheTenantsLanguagesFromTheAdmin(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	html := dashboard(t, cfg, admin, "pt-PT,pt;q=0.9")
	link := regexp.MustCompile(`href="(/app/[^"]*languages[^"]*)"`).FindStringSubmatch(html)
	if link == nil {
		t.Fatalf("the admin offers its administrator no link to the tenant's languages")
	}
	status, ctype, _, page := r7navigate(t, cfg, admin, acmeHost, link[1], "en")
	if status != http.StatusOK || !strings.Contains(ctype, "text/html") {
		t.Fatalf("GET %s = %d %s: %s", link[1], status, ctype, firstLineOf(page))
	}
	for _, tag := range []string{"en", "pt-PT"} {
		if !regexp.MustCompile(`value="` + regexp.QuoteMeta(tag) + `"`).MatchString(page) {
			t.Errorf("the languages page at %s offers no control for the tenant's %s", link[1], tag)
		}
	}
}
