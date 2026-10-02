package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestThePortugueseAdminShellWritesNoEnglishChrome: the brief moves navigation, the
// account menu and the shell's buttons into the catalogue, so a page declared
// pt-PT carries none of the shell's English words as text or accessible names.
func TestThePortugueseAdminShellWritesNoEnglishChrome(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)
	html := dashboard(t, cfg, admin, "pt-PT,pt;q=0.9")
	if got := pageLang(html); got != "pt-PT" || !strings.Contains(html, "Painel") {
		t.Fatalf("the fixture's dashboard is not the Portuguese one (lang=%q): %s", got, firstLineOf(html))
	}
	for _, english := range []string{"Dashboard", "Health", "Components", "Sign out", "Admin navigation",
		"Switch between the light and dark theme", "Are you sure?", "no roles"} {
		text := regexp.MustCompile(`>\s*` + regexp.QuoteMeta(english) + `\s*<`)
		attr := regexp.MustCompile(`(aria-label|title)="` + regexp.QuoteMeta(english) + `"`)
		if text.MatchString(html) || attr.MatchString(html) {
			t.Errorf("the pt-PT admin shell still writes the English %q", english)
		}
	}
}
