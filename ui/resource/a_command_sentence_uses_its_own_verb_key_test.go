package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandSentenceUsesItsOwnVerbKey(t *testing.T) {
	t.Parallel()

	var keys []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			keys = append(keys, key)
			return "translated: " + fallback
		})
	got := words.Command("archive", entity.CommandHints{Label: "Archive", SuccessMessage: "The note is archived."})

	if got.SuccessMessage != "translated: The note is archived." {
		t.Errorf("success message = %q, want translated declared sentence", got.SuccessMessage)
	}
	if len(keys) != 2 || keys[1] != "hints.notes/note.command.archive.successMessage" {
		t.Errorf("translation keys = %v, want label then archive-specific success key", keys)
	}
}
