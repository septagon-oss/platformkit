package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandConfirmationUsesItsCallersTranslationScope(t *testing.T) {
	t.Parallel()

	var keys []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			keys = append(keys, key)
			return "translated: " + fallback
		})
	got := words.Command("archive", entity.CommandHints{
		Confirmation: &entity.CommandConfirmation{
			Title:        "Archive note?",
			Body:         "This cannot be undone.",
			ConfirmLabel: "Archive",
		},
	})

	if got.Confirmation == nil {
		t.Fatal("confirmation = nil, want translated confirmation")
	}
	if got.Confirmation.Title != "translated: Archive note?" ||
		got.Confirmation.Body != "translated: This cannot be undone." ||
		got.Confirmation.ConfirmLabel != "translated: Archive" {
		t.Errorf("confirmation = %#v, want all declared words translated", got.Confirmation)
	}
	want := []string{
		"hints.notes/note.command.archive.confirmation.title",
		"hints.notes/note.command.archive.confirmation.body",
		"hints.notes/note.command.archive.confirmation.confirmLabel",
	}
	if len(keys) != len(want) {
		t.Fatalf("translation keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("translation key %d = %q, want %q", i, keys[i], want[i])
		}
	}
}
