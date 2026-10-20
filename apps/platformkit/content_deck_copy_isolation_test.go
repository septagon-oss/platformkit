package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestContentDeckCopyStaysIndependentAcrossLanguagesAndPermissions(t *testing.T) {
	content := resourceAt(t, "content", "content")
	for _, tc := range []struct {
		language, publish, archive string
	}{
		{"pt-PT", "Publicar", "Arquivar"},
		{"en", "Publish", "Archive"},
		{"pt-PT", "Publicar", "Arquivar"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			reader := content
			reader.Commands = nil
			entry := screens.Describe1(screens.Localise(reader, words(t, tc.language)), false)
			if entry.Presentation == nil || entry.Presentation.Deck == nil {
				t.Fatal("reader has no deck")
			}
			deck := entry.Presentation.Deck
			if len(deck.Directions) != 1 || !deck.Directions["down"].Skip {
				t.Fatalf("reader directions = %+v, want only local skip", deck.Directions)
			}
			if len(deck.Queue) != 1 || deck.Queue[0] != "status:draft" {
				t.Fatalf("reader queue = %v, want status:draft", deck.Queue)
			}
			deck.Queue[0] = "status:archived"
			delete(deck.Directions, "down")

			entry = screens.Describe1(screens.Localise(content, words(t, tc.language)), true)
			if entry.Presentation == nil || entry.Presentation.Deck == nil {
				t.Fatal("manager has no deck")
			}
			deck = entry.Presentation.Deck
			if len(deck.Queue) != 1 || deck.Queue[0] != "status:draft" || deck.Order != "updatedAt" {
				t.Fatalf("manager queue or order changed: %+v", deck)
			}
			if len(deck.Directions) != 3 || !deck.Directions["down"].Skip ||
				deck.Directions["right"].Command != "publish" || deck.Directions["right"].Label != tc.publish ||
				deck.Directions["left"].Command != "archive" || deck.Directions["left"].Label != tc.archive {
				t.Fatalf("manager directions = %+v, want localized publish, archive and skip", deck.Directions)
			}
			deck.Queue[0] = "status:published"
			delete(deck.Directions, "right")
		})
	}
	if content.Present.Deck.Directions["right"].Label != "Publish" || content.Present.Deck.Queue[0] != "status:draft" {
		t.Fatalf("registered deck changed: %+v", content.Present.Deck)
	}
}
