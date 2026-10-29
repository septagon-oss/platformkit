package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandConfirmationUsesItsCallersVerb(t *testing.T) {
	t.Parallel()

	var keys []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			keys = append(keys, key)
			return "translated: " + fallback
		})
	got := words.Command("archive", entity.CommandHints{
		Confirmation: &entity.CommandConfirmation{Title: "Archive note?"},
	})

	if got.Confirmation == nil || got.Confirmation.Title != "translated: Archive note?" {
		t.Fatalf("confirmation = %#v, want translated declared title", got.Confirmation)
	}
	if len(keys) != 1 || keys[0] != "hints.notes/note.command.archive.confirmation.title" {
		t.Errorf("translation keys = %v, want archive-specific confirmation title key", keys)
	}
}
