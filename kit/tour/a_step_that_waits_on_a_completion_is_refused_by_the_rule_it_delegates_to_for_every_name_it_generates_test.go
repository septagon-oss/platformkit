package tour_test

// A step that waits for a completion is refused in one sentence whose verdict is
// screen's: kit/tour asks screen.Segment and copies that rule's clauses into its
// own words. That equivalence is asked over a generated name space, and a name
// space kept in one file can be trimmed by the next hand on it — every answer
// then agrees again over whatever is left. So this file generates names of its
// own, one byte in each form the rule reads, and asks each of both halves: the
// verdict screen.Segment gives must be the verdict the declaration gives, and a
// refusal must name every clause of its own sentence the name breaks. A name the
// two answer differently, or a refusal that leaves a broken clause out, is a tour
// an author cannot fix by reading the answer it was given.

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// sentenceClause is one clause of the refusal sentence: its own words, and the
// test those words make of a completion's operation name, run without screen.
type sentenceClause struct {
	says   string
	breaks func(name string) bool
}

func TestAStepThatWaitsOnACompletionIsRefusedByTheRuleItDelegatesToForEveryNameItGenerates(t *testing.T) {
	clauses := []sentenceClause{
		{"non-empty", func(name string) bool { return name == "" }},
		{"valid UTF-8", func(name string) bool { return !utf8.ValidString(name) }},
		{"holds no control character", func(name string) bool { return strings.ContainsFunc(name, unicode.IsControl) }},
	}
	for _, name := range completionNames() {
		_, err := screen.Segment(name)
		broken := brokenClauses(clauses, name)
		if (err != nil) != (len(broken) > 0) {
			t.Errorf("screen refuses %q = %v, the clauses of the refusal sentence refuse it = %v (%s): the sentence no longer states the whole rule",
				name, err != nil, len(broken) > 0, strings.Join(broken, ", "))
			continue
		}
		if err == nil {
			continue
		}
		problems := waitingForCompletion(tour.OperationRef(name)).DeclarationProblems("feature")
		want := fmt.Sprintf("operation %q is not a name an operation can be mounted under", name)
		if len(problems) != 1 || !strings.Contains(problems[0], want) {
			t.Errorf("refusal of %q = %q, want one sentence holding %q", name, problems, want)
			continue
		}
		for _, clause := range broken {
			if !strings.Contains(problems[0], clause) {
				t.Errorf("refusal of %q = %q leaves out %q, a clause the rule refused it for", name, problems, clause)
			}
		}
	}
}

// completionNames is a name space of this file's own: whole names, one byte in
// each form the rule reads, and the names that break two clauses at once.
func completionNames() []string {
	names := []string{"", "\x00", "\xff", "\xff\xfe", "\xff\x00", "\x00\xff", "feature",
		"feature-note-create", "feature-\xff\x00-note", "feature\x00note", strings.Repeat("x", 4096)}
	for b := range 256 {
		names = append(names, "feature-"+string(rune(b))+"-note",
			"feature-"+string([]byte{byte(b)})+"-note", string(rune(b)), string([]byte{byte(b)}))
	}
	return names
}

// brokenClauses names, in the sentence's own words, each clause of it a name fails.
func brokenClauses(clauses []sentenceClause, name string) []string {
	var broken []string
	for _, clause := range clauses {
		if clause.breaks(name) {
			broken = append(broken, clause.says)
		}
	}
	return broken
}

// waitingForCompletion is a declaration with one spotlight step and nothing else,
// so the answer a name gets is the name's own.
func waitingForCompletion(operation tour.OperationRef) tour.Tour {
	return tour.Tour{
		Ref:   tour.Ref{ID: "feature.first", Edition: 1},
		Title: tour.Message{Key: "feature.first", Fallback: "Add your first note"},
		Steps: []tour.Step{tour.Spotlight("save", "app/feature/custom/editor#action.submit",
			tour.Content{Title: tour.Message{Key: "feature.first.step.save", Fallback: "Save it"},
				Body: tour.Message{Key: "feature.first.step.save.body", Fallback: "Saving writes the note."}},
			tour.Completes(operation), tour.AnchoredOptions{WhenMissing: tour.Skip})},
	}
}
