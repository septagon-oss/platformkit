package tour_test

// screen.Segment is the rule a completion's operation name is held to, and it
// refuses three shapes: a name that is empty, one that holds bytes no UTF-8
// decoder reads, and one that holds a control character. The sentence this refusal
// answers with names all three, so every shape it names is asked here — a clause
// with nothing behind it reads to an author as a refusal with nothing left to fix,
// and a clause that quietly leaves the sentence leaves this refused with it. Each
// shape is also asked of screen.Segment, so a rule whose extent moves reports a
// stale table rather than a refusal that never arrived.

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestAnOperationNameIsRefusedForEveryShapeItsSentenceNames(t *testing.T) {
	const sentence = `tour "feature.first", step "save": operation %q is not a name an operation can be mounted under; a name is non-empty, valid UTF-8 and holds no control character`
	for _, tc := range []struct {
		shape   string
		name    string
		refused bool
	}{
		{"empty", "", true},
		{"not valid UTF-8", "feature-note\xff-created", true},
		{"a newline inside", "feature-note\n-created", true},
		{"a NUL inside", "feature-note\x00-created", true},
		{"a name a step may wait a completion on", "screen-feature-note-create", false},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			_, err := screen.Segment(tc.name)
			if (err != nil) != tc.refused {
				t.Fatalf("screen.Segment refuses %q = %v; this table holds that shape as refused = %v", tc.name, err != nil, tc.refused)
			}
			problems := completionTour(tour.OperationRef(tc.name)).DeclarationProblems("feature")
			if !tc.refused {
				if len(problems) != 0 {
					t.Fatalf("the declaration refused a name the screen rule accepts: %v", problems)
				}
				return
			}
			if want := fmt.Sprintf(sentence, tc.name); len(problems) != 1 || problems[0] != want {
				t.Fatalf("refusal = %q, want %q", problems, want)
			}
		})
	}
}

// completionTour is the fixture with only its completion changed, so the answer is
// the operation name's and nothing else's.
func completionTour(operation tour.OperationRef) tour.Tour {
	declared := fixtureTour()
	replace(&declared, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
		tour.Content{
			Title: text("feature.first.step.save", "Save it"),
			Body:  text("feature.first.step.save.body", "Saving writes the note and takes you to it."),
		}, tour.Completes(operation), tour.AnchoredOptions{WhenMissing: tour.Skip}))
	return declared
}
