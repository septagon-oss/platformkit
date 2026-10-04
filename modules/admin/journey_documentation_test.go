package admin_test

import (
	"os"
	"regexp"
	"testing"
)

func TestDocumentedAdminJourneyLinkResolves(t *testing.T) {
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(`\[[^\]]*e2e/steps/admin\.ts\]\(([^)]+)\)`).FindSubmatch(body)
	if len(link) != 2 {
		t.Fatal("the admin README must link its exported journey steps")
	}
	if _, err := os.Stat(string(link[1])); err != nil {
		t.Errorf("documented journey link %q does not resolve relative to modules/admin/README.md: %v", link[1], err)
	}
}
