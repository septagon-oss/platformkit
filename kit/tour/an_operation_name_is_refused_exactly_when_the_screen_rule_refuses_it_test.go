package tour_test

// A step that waits for a completion names the operation by the rule screen
// holds a published operation id to: kit/tour asks screen.Segment and refuses
// with a sentence that states the whole of that rule. A named list of shapes
// cannot hold that promise, because the rule lives in screen and can grow a
// clause no list knows about — the declaration would then refuse names its own
// sentence does not describe. So every name here is generated, and each one is
// asked of both halves: the verdict screen.Segment gives must be the verdict the
// declaration gives, and where they refuse together the sentence must be the one
// the declaration answers with. A name the two answer differently, or a refusal
// whose sentence leaves a clause out, is a tour an author cannot fix by reading
// the refusal it was given.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestAnOperationNameIsRefusedExactlyWhenTheScreenRuleRefusesIt(t *testing.T) {
	const sentence = `tour "feature.first", step "save": operation %q is not a name an operation can be mounted under; a name is non-empty, valid UTF-8 and holds no control character`
	for _, name := range operationNames() {
		_, err := screen.Segment(name)
		problems := waitingOn(tour.OperationRef(name)).DeclarationProblems("feature")
		if (err != nil) != (len(problems) > 0) {
			t.Errorf("screen.Segment refuses %q = %v, the declaration refuses it = %v (%v): the two rules answer differently",
				name, err != nil, len(problems) > 0, problems)
			continue
		}
		if err == nil {
			continue
		}
		if want := fmt.Sprintf(sentence, name); len(problems) != 1 || problems[0] != want {
			t.Errorf("refusal of %q = %q, want %q", name, problems, want)
		}
	}
}

// operationNames is the name space a completion can be written with, generated
// rather than chosen: one byte of every code point the rule reads sits inside an
// otherwise valid name, so no clause a rule can grow goes unasked.
func operationNames() []string {
	names := []string{"", "feature-note-create", "feature", "screen-feature-note-create"}
	for b := 0; b <= 0x7f; b++ {
		names = append(names, "feature-"+string(rune(b))+"-note")
	}
	for _, r := range []rune{'\u0080', '\u0085', '\u009f', '\u00a0', '\u2028', '\ufeff', '日', 'é'} {
		names = append(names, "feature-"+string(r)+"-note")
	}
	names = append(names, "feature-\xff-note", "feature-\xc3-note", "\xff\xfe", "\x00", "feature\n\r\x00\x7f-note")
	for n := 1; n <= 1024; n *= 2 {
		names = append(names, "feature-note-"+strings.Repeat("x", n))
	}
	return names
}

// waitingOn is a declaration with one spotlight step and nothing else, so the
// answer a name gets is the name's own.
func waitingOn(operation tour.OperationRef) tour.Tour {
	return tour.Tour{
		Ref:   tour.Ref{ID: "feature.first", Edition: 1},
		Title: tour.Message{Key: "feature.first", Fallback: "Add your first note"},
		Steps: []tour.Step{
			tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{
					Title: tour.Message{Key: "feature.first.step.save", Fallback: "Save it"},
					Body:  tour.Message{Key: "feature.first.step.save.body", Fallback: "Saving writes the note and takes you to it."},
				},
				tour.Completes(operation), tour.AnchoredOptions{WhenMissing: tour.Skip}),
		},
	}
}
