package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestACommandSuccessSentenceUsesItsDeclaredTranslationKey(t *testing.T) {
	t.Parallel()

	var asked []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			asked = append(asked, key)
			return "translated: " + fallback
		})
	got := words.Command("publish", entity.CommandHints{
		Label: "Publish", SuccessMessage: "The note is published.",
	})

	if got.SuccessMessage != "translated: The note is published." {
		t.Errorf("success message = %q, want translated declared sentence", got.SuccessMessage)
	}
	if len(asked) != 2 || asked[1] != "hints.notes/note.command.publish.successMessage" {
		t.Errorf("formatter keys = %v, want label then declared command success key", asked)
	}
}
