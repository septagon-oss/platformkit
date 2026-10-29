package forms_test

// The drawing control carries its English twice over: the sentence it hands its copy
// seam as a default, and the entry a language catalogue keeps under the same key. The
// seam keeps the default when no catalogue answers — including when the app composes no
// copy seam at all, which is the one case `p.Text == nil` decides in the control itself.
//
// Every case beside this file drives the board through a seam that answers, so the half
// of the board a person reads when nothing is wired is unpinned: a control whose nil
// branch dropped a sentence, or answered it from a wording that no catalogue keeps,
// renders a board no page test reads, because every page in the tree wires a catalogue
// that answers first. This renders that board and asks it for the English the English
// catalogue states, sentence by sentence, so the two copies and the branch that keeps
// one of them are asked about together.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// TestTheSketchBoardWithNoCopySeamShowsTheEnglishItsCatalogueKeeps renders one
// required drawing control with no copy seam wired at all — the app that composes no
// translation module — and asks the rendered markup for every sentence the English
// catalogue keeps for a drawing field. A sentence that reaches no markup, or reaches it
// in a wording the catalogue does not keep, is what such an app hands a person, since
// the control's own English is the only wording it has. Required, so the line a field
// adds when it must be filled is asked for too.
func TestTheSketchBoardWithNoCopySeamShowsTheEnglishItsCatalogueKeeps(t *testing.T) {
	english := sketchLanguage(t, "en.json")
	board := strings.Builder{}
	err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch, Required: true}, Label: "Drawing"},
	}).Render(&board)
	if err != nil {
		t.Fatalf("a drawing control with no copy seam could not render: %v", err)
	}
	markup := board.String()
	asked := 0
	for key, sentence := range english {
		if !strings.HasPrefix(key, "forms.sketch.") || strings.TrimSpace(sentence) == "" {
			continue
		}
		asked++
		if !strings.Contains(markup, sentence) {
			t.Errorf("a drawing control with no copy seam shows none of %q, the sentence en.json keeps for %s: with no catalogue to answer, this markup is what a person is handed", sentence, key)
		}
	}
	if asked < 10 {
		t.Fatalf("en.json keeps %d sentences for a drawing field, too few to ask about", asked)
	}
}
