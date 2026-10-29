package forms_test

// A drawing control carries its own English: every sentence it asks the shell for
// travels with a default the control keeps in its Go source, and that default is what
// a person reads when the app composes no translation module — the copy seam is one
// argument on a form, and a caller that passes none asks no catalogue at all. The
// English catalogue is a second copy of the same sentences, written apart from the
// control: the literal beside the key in ui/forms/sketch.go and the entry in
// ui/forms/messages/en.json.
//
// Which half a delivery rewords is decided apart too. Reword the sentence a page shows
// and the rendered markup moves, which the cases beside this file read; reword the
// control's own default and no markup moves anywhere, because every page in the tree
// wires a catalogue that answers first. Then the app that composes a translation module
// answers one wording and the app that composes none answers another, under the same
// key, with nothing in this package noticing.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// sketchAsk is one sentence a control asks the shell for, as it asks it: the key it
// builds and the English it carries beside that key.
type sketchAsk struct{ key, fallback string }

// TestTheSketchControlHandsTheShellTheEnglishItsCatalogueStates records every key and
// default one drawing control puts to its copy seam, in the shapes a page renders it in
// — required and blank, holding a value the server could not read, refused with a field
// error, held immutable, and holding a drawing it can read — and refuses a default that
// is not the sentence the English catalogue keeps under that key. The asks come off the
// control's own call rather than a list kept here, and the floor of ten names how few
// asks this control makes today, so a board that stopped carrying English at all is
// refused rather than passing on an empty set.
func TestTheSketchControlHandsTheShellTheEnglishItsCatalogueStates(t *testing.T) {
	asks := sketchDefaultsTheControlCarries(t)
	if len(asks) < 10 {
		t.Fatalf("the drawing control carries English for %d sentences, too few to ask about: %v", len(asks), asks)
	}
	english := sketchCatalogues(t)["en.json"]
	for _, ask := range asks {
		said, carried := strings.TrimSpace(english[ask.key]), strings.TrimSpace(ask.fallback)
		if said == "" || carried == "" || said != carried {
			t.Errorf("the drawing control carries %q as the English for %s, where en.json keeps %q: an app that composes no translation module answers in the control's own words, so the two wordings are what a person is handed, one per app", ask.fallback, ask.key, english[ask.key])
		}
	}
}

// sketchDefaultsTheControlCarries is every (key, default) pair one drawing control puts
// to the copy seam. The seam answers with the default it is handed and only records the
// ask, so this decides nothing about what the control has to say — only that whatever it
// says beside a key is what the English catalogue says under it.
func sketchDefaultsTheControlCarries(t *testing.T) []sketchAsk {
	t.Helper()
	drawing := `[[[10,20,0],[30,40,25]]]`
	var asks []sketchAsk
	seen := map[string]bool{}
	for _, props := range []forms.ControlProps{
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch, Required: true}, Label: "Drawing"}},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Value: "not a drawing"},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Error: "The saved drawing could not be read."},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Value: drawing, Immutable: true},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Value: drawing},
	} {
		props.Text = func(key, fallback string) string {
			if !seen[key] {
				seen[key] = true
				asks = append(asks, sketchAsk{key: key, fallback: fallback})
			}
			return fallback
		}
		var out strings.Builder
		if err := forms.Control(props).Render(&out); err != nil {
			t.Fatalf("a drawing control could not render: %v", err)
		}
	}
	return asks
}
