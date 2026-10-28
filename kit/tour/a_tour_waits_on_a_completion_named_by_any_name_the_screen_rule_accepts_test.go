package tour_test

// A completion's operation name is refused by kit/tour exactly when the screen
// rule it delegates to refuses it. The name spaces beside this file ask that of
// a byte sitting inside an otherwise valid name; none asks a name that is the
// whole of what an author writes — one accepted character, a name of sixty-five
// thousand bytes, a name that spells a percent-escape or a path boundary of
// itself. Those are shapes the two halves can answer differently, so this file
// asks a space of whole names: a name the rule accepts must leave the
// declaration silent, and one it refuses must leave a refusal. An accepted name
// the declaration refuses is a completion a tour could mount and never name, and
// no refusal would explain it; the sentence's own wording is pinned beside it.
import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestATourWaitsOnACompletionNamedByAnyNameTheScreenRuleAccepts(t *testing.T) {
	for _, name := range wholeOperationNames() {
		_, err := screen.Segment(name)
		problems := tourWaitingOn(tour.OperationRef(name)).DeclarationProblems("feature")
		if (err != nil) != (len(problems) > 0) {
			t.Errorf("screen's rule refuses %q (len %d) = %v, the tour refuses it = %v (%v): the two rules answer a whole name differently",
				name, len(name), err != nil, len(problems) > 0, problems)
		}
	}
}

// wholeOperationNames is a whole name per shape the rule reads: each byte on its
// own, decoded and raw, names that decode to something else, the separators a
// path or a query would read, and lengths past any bound a rule could grow.
func wholeOperationNames() []string {
	names := []string{"feature", "feature-note-create", ".", "..", "/", "%", "%41", "note%2Fcreate",
		"note create", "note/create", "note.create", "note:create", "note?create", "note#create",
		strings.Repeat("x", 1025), strings.Repeat("x", 65536)}
	for b := range 256 {
		names = append(names, string(rune(b)), string([]byte{byte(b)}))
	}
	return names
}

// tourWaitingOn is a declaration with one spotlight step and nothing else, so the
// answer a name gets is the name's own.
func tourWaitingOn(operation tour.OperationRef) tour.Tour {
	return tour.Tour{
		Ref:   tour.Ref{ID: "feature.first", Edition: 1},
		Title: tour.Message{Key: "feature.first", Fallback: "Add your first note"},
		Steps: []tour.Step{tour.Spotlight("save", "app/feature/custom/editor#action.submit",
			tour.Content{Title: tour.Message{Key: "feature.first.step.save", Fallback: "Save it"},
				Body: tour.Message{Key: "feature.first.step.save.body", Fallback: "Saving writes the note."}},
			tour.Completes(operation), tour.AnchoredOptions{WhenMissing: tour.Skip})},
	}
}
