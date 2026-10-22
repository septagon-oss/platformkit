package screens_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestUndoableCatalogueRequiresTheAuthorizedUndoCommand(t *testing.T) {
	r := reviewResource()
	r.Present.Deck.Undoable = true
	for i := range r.Commands {
		r.Commands[i].Undo, r.Commands[i].HasResult = "undo", true
	}
	r.Commands = append(r.Commands, httpx.Command{Verb: "undo"})
	full := screens.Describe1(r, true)
	if !full.Presentation.Deck.Undoable || full.Presentation.Deck.Directions["right"].Undo != "undo" {
		t.Fatalf("undo omitted: %+v", full.Presentation.Deck)
	}
	r.Commands = r.Commands[:len(r.Commands)-1]
	filtered := screens.Describe1(r, true)
	if filtered.Presentation.Deck.Undoable || filtered.Presentation.Deck.Directions["right"].Undo != "" {
		t.Fatalf("unavailable undo advertised: %+v", filtered.Presentation.Deck)
	}
}
