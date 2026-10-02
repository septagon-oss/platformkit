package main

import (
	"regexp"
	"testing"
)

// TestTheAccountMenuOffersOnlyTheTenantsLanguages: the account menu has a language
// control, it lists the tenant's languages, and nothing else.
func TestTheAccountMenuOffersOnlyTheTenantsLanguages(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)
	html := dashboard(t, cfg, admin, "pt-PT,pt;q=0.9")
	if got := pageLang(html); got != "pt-PT" {
		t.Fatalf("the fixture's dashboard declares lang=%q: %s", got, firstLineOf(html))
	}
	// Any control named locale — a select's options or radio inputs — counts.
	offered := map[string]bool{}
	for _, select_ := range regexp.MustCompile(`(?s)<select[^>]*name="locale"[^>]*>(.*?)</select>`).FindAllStringSubmatch(html, -1) {
		for _, v := range regexp.MustCompile(`<option[^>]*value="([^"]*)"`).FindAllStringSubmatch(select_[1], -1) {
			if v[1] != "" {
				offered[v[1]] = true
			}
		}
	}
	for _, input := range regexp.MustCompile(`<input[^>]*name="locale"[^>]*>`).FindAllString(html, -1) {
		if v := regexp.MustCompile(`value="([^"]+)"`).FindStringSubmatch(input); v != nil {
			offered[v[1]] = true
		}
	}
	if len(offered) == 0 {
		t.Fatalf("the signed-in admin page has no account language control (a control named locale)")
	}
	if !offered["pt-PT"] || !offered["en"] || len(offered) != 2 {
		t.Errorf("the language control offers %v, want exactly the tenant's pt-PT and en", offered)
	}
}
