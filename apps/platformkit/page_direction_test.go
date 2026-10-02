package main

import (
	"regexp"
	"testing"
)

// TestTheAdminPageDeclaresTheChosenLanguagesDirection: <html lang> and dir both
// follow the language a page is written in; Portuguese is written left to right.
func TestTheAdminPageDeclaresTheChosenLanguagesDirection(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)
	html := dashboard(t, cfg, admin, "pt-PT,pt;q=0.9")
	root := regexp.MustCompile(`(?s)<html[^>]*>`).FindString(html)
	if pageLang(html) != "pt-PT" {
		t.Fatalf("the fixture's dashboard is not in pt-PT: %s", root)
	}
	if !regexp.MustCompile(`\bdir="ltr"`).MatchString(root) {
		t.Errorf("the pt-PT page's root element %s declares no dir=\"ltr\"", root)
	}
}
