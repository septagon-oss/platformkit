package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandConfirmationTranslationKeyTracksVerb(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"publish", "archive"} {
		t.Run(verb, func(t *testing.T) {
			var keys []string
			words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
				func(key, fallback string) string {
					keys = append(keys, key)
					return fallback
				})
			words.Command(verb, entity.CommandHints{Confirmation: &entity.CommandConfirmation{
				Title: "Confirm",
			}})

			want := "hints.notes/note.command." + verb + ".confirmation.title"
			if len(keys) != 1 || keys[0] != want {
				t.Fatalf("confirmation translation keys = %v, want [%q]", keys, want)
			}
		})
	}
}
