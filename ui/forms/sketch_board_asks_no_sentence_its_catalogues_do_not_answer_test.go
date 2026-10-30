package forms_test

// The drawing board keeps no words of its own: it asks the shell for every sentence
// under a key it builds itself, and the shell answers from the request's catalogue or,
// holding no such entry, with the English the control carries as a default. That seam
// runs both ways, and only one way was asked about. Whether a sentence a catalogue keeps
// reaches the board is asked beside this file; what nothing asked is whether every key
// the control asks is a key some catalogue answers. A key no catalogue answers is
// invisible to the tree: the board's attributes still stand, every catalogue stays
// complete for the names it holds, and a person reading the page in Portuguese is handed
// the control's own English in every language — a default is not a translation nobody
// wrote.
//
// The keys come off the control's own call to the copy seam rather than a list kept
// here, so a sentence the board starts asking, or one it renames, is refused without
// this file being edited.

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// TestTheSketchBoardAsksNoSentenceItsCataloguesDoNotAnswer drives one drawing control
// through a copy seam that records each key it is asked, in the three shapes a page
// shows it in — blank and required, holding a value the server could not read, and
// refused with a field error — and refuses a key that a language the portable form
// controls ship leaves unanswered. In a language other than English the answer must be
// that language's own sentence: a catalogue that repeats the English translates nothing.
func TestTheSketchBoardAsksNoSentenceItsCataloguesDoNotAnswer(t *testing.T) {
	asked := sketchKeysTheBoardAsks(t)
	if len(asked) < 10 {
		t.Fatalf("the drawing board asked the copy seam for %d sentences, too few to ask about: %v", len(asked), asked)
	}
	catalogues := sketchCatalogues(t)
	english := catalogues["en.json"]
	for _, key := range asked {
		if !strings.HasPrefix(key, "forms.sketch.") {
			t.Errorf("the drawing board asks the copy seam for %q, which is no key the drawing control owns", key)
		}
		for language, catalogue := range catalogues {
			said := strings.TrimSpace(catalogue[key])
			if said == "" {
				t.Errorf("the %s catalogue answers no %s, which the drawing board asks for: whoever reads this board is handed the control's own English default", language, key)
				continue
			}
			if language != "en.json" && said == strings.TrimSpace(english[key]) {
				t.Errorf("the %s catalogue repeats the English sentence %q for %s rather than translating it", language, said, key)
			}
		}
	}
}

// sketchKeysTheBoardAsks is every key one drawing control puts to the copy seam, in the
// shapes a page renders it in: required and blank, so the line a field adds when it must
// be filled is asked; holding text the server could not read, so the line the board says
// in its place is asked; and refused, so the shapes of a save that could not be applied
// are covered. The seam answers with the control's own default and only records the ask,
// so this file decides nothing about what a catalogue has to hold.
func sketchKeysTheBoardAsks(t *testing.T) []string {
	t.Helper()
	var asked []string
	seen := map[string]bool{}
	for _, props := range []forms.ControlProps{
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch, Required: true}, Label: "Drawing"}},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Value: "not a drawing"},
		{Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"}, Error: "The saved drawing could not be read."},
	} {
		var out strings.Builder
		props.Text = func(key, fallback string) string {
			if !seen[key] {
				seen[key] = true
				asked = append(asked, key)
			}
			return fallback
		}
		if err := forms.Control(props).Render(&out); err != nil {
			t.Fatalf("a drawing control could not render: %v", err)
		}
	}
	slices.Sort(asked)
	return asked
}
