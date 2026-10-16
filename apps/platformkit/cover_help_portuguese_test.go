package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheCoverPictureHelpIsDeclaredAndAnsweredInPortuguese checks that the
// content cover field carries its help as a declared hint, so the generated
// form says it in the request's language, and that the composed pt-PT catalog
// answers the key the resolver derives for it with a Portuguese sentence.
func TestTheCoverPictureHelpIsDeclaredAndAnsweredInPortuguese(t *testing.T) {
	r := resourceAt(t, "content", "content")
	declared := ""
	for _, f := range r.Schema.Fields {
		if f.Name == "coverImageId" {
			declared = f.Presentation.Help
		}
	}
	if strings.TrimSpace(declared) == "" {
		t.Fatalf("content.coverImageId declares no help; the form would print none, or an undeclared literal")
	}
	selected := page.SelectLocale(page.Messages(catalogues()), "pt-PT")
	var asked []string
	var text resource.Text = func(key, fallback string) string {
		if strings.Contains(key, "coverImageId.help") {
			asked = append(asked, key)
			return selected.Formatter.Text(key, unfound)
		}
		return fallback
	}
	words := resource.WordsFor(r.Schema, text)
	words.Fields(r.Schema.Fields)
	if len(asked) != 1 {
		t.Fatalf("the resolver asked for the cover help %d times (%v), want once", len(asked), asked)
	}
	got := selected.Formatter.Text(asked[0], unfound)
	if got == unfound || got == declared {
		t.Errorf("pt-PT answers %s with %q; want the Portuguese help, not the English %q", asked[0], got, declared)
	}
}
