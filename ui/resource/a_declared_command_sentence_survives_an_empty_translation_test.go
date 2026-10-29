package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestADeclaredCommandSentenceSurvivesAnEmptyTranslation(t *testing.T) {
	t.Parallel()

	var asked []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			asked = append(asked, key)
			return ""
		})
	got := words.Command("publish", entity.CommandHints{
		Label: "Publish", SuccessMessage: "The note is published.",
	})

	if got.SuccessMessage != "The note is published." {
		t.Errorf("an empty translation removed the declared success sentence: got %q", got.SuccessMessage)
	}
	if len(asked) != 2 {
		t.Errorf("the formatter was asked for %d declared command words, want 2: %v", len(asked), asked)
	}
}
