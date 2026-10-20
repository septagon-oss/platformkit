package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A tenant is provisioned as it is created: seed/starter/contents.yaml writes its
// pages through the content module in the transaction that made the tenant, so the
// content list a person opens has rows from the first minute. A browser journey that
// opens that list and waits for its empty state waits for a page this application
// never serves, and fails on its own precondition rather than on what it set out to
// measure. The empty-state words are asked of a list nothing provisions.
func TestNoJourneyWaitsForTheEmptyStateOfTheSeededContentList(t *testing.T) {
	starter, err := os.ReadFile(filepath.Join("seed", "starter", "contents.yaml"))
	if err != nil {
		t.Fatalf("read the starter seed: %v", err)
	}
	pages := len(regexp.MustCompile(`(?m)^\s*- key:`).FindAll(starter, -1))
	if pages == 0 {
		t.Fatal("seed/starter/contents.yaml declares no page; this case assumes a provisioned tenant has content")
	}

	specs, err := filepath.Glob(filepath.Join("..", "..", "e2e", "*.spec.ts"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("find the browser journeys: %v (%d found)", err, len(specs))
	}
	opensTheList := regexp.MustCompile(`goto\(\s*['"]/app/content/contents['"]\s*\)`)
	waitsForEmpty := regexp.MustCompile(`No content yet\.|['"]0 content['"]`)
	for _, spec := range specs {
		body, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		if !opensTheList.Match(body) {
			continue
		}
		if found := waitsForEmpty.Find(body); found != nil {
			t.Errorf("%s opens /app/content/contents, which the starter seed fills with %d page(s) as the tenant is created, and waits for its empty state (%s); ask a list nothing provisions for the empty words, or ask this one for the rows it has",
				filepath.Base(spec), pages, found)
		}
	}
}
