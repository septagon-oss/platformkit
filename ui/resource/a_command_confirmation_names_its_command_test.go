package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandConfirmationNamesItsCommand(t *testing.T) {
	t.Parallel()

	var keys []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			keys = append(keys, key)
			return fallback
		})
	words.Command("publish", entity.CommandHints{Confirmation: &entity.CommandConfirmation{
		Title: "Publish note?",
	}})

	want := []string{"hints.notes/note.command.publish.confirmation.title"}
	if len(keys) != len(want) {
		t.Fatalf("translation keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("translation key %d = %q, want %q", i, keys[i], want[i])
		}
	}
}
