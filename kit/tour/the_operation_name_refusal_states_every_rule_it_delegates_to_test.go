package tour_test

// A step that waits for a completion is refused in a sentence that lists three
// clauses, and the rule behind them lives in kit/screen, where it can grow a
// fourth. The sentence and the rule are then two texts with no shared source:
// a shape only the new clause refuses is refused by both halves, so the verdicts
// agree and the author is told a reason the code does not run. This file keeps
// them from parting. The clauses below are the wording of the sentence, each one
// probes the name on its own, and every generated name is asked of the rule and
// of the clauses together. A name the rule refuses that no clause refuses, or a
// refusal whose sentence omits the clause that caught it, is a tour an author
// cannot fix by reading the refusal it was given.
//
// The clause list copies the sentence on purpose: when the rule grows, the answer
// is to state the new clause where an author can read it and add it here, not to
// leave the refusal saying less than it runs.

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// clause is one phrase of the refusal sentence and the test that phrase makes of
// a completion's operation name, run without asking screen.
type clause struct {
	says    string
	breaks  func(name string) bool
	witness string // a name this clause alone, and no other here, breaks
}

func TestTheOperationNameRefusalStatesEveryRuleItDelegatesTo(t *testing.T) {
	clauses := []clause{
		{says: "non-empty", breaks: func(name string) bool { return name == "" }, witness: ""},
		{says: "valid UTF-8", breaks: func(name string) bool { return !utf8.ValidString(name) }, witness: "feature-\xff-note"},
		{says: "holds no control character", breaks: func(name string) bool { return strings.ContainsFunc(name, unicode.IsControl) }, witness: "feature-\u007f-note"},
	}
	for _, name := range operationNames() {
		_, err := screen.Segment(name)
		stated := statedProblems(clauses, name)
		if (err != nil) != (len(stated) > 0) {
			t.Errorf("screen refuses %q = %v, the clauses the refusal sentence states refuse it = %v (%s): the sentence no longer states the whole rule a completion's operation name is held to",
				name, err != nil, len(stated) > 0, strings.Join(stated, ", "))
		}
	}
	for _, c := range clauses {
		if got := statedProblems(clauses, c.witness); len(got) != 1 || got[0] != c.says {
			t.Errorf("witness %q is broken by %v, want only %q", c.witness, got, c.says)
			continue
		}
		problems := waitingOn(tour.OperationRef(c.witness)).DeclarationProblems("feature")
		want := fmt.Sprintf("operation %q is not a name an operation can be mounted under", c.witness)
		if len(problems) != 1 || !strings.Contains(problems[0], want) || !strings.Contains(problems[0], c.says) {
			t.Errorf("refusal of %q = %q, want one sentence naming %q as the clause it broke", c.witness, problems, c.says)
		}
	}
}

// statedProblems names the clauses of the refusal sentence a name breaks.
func statedProblems(clauses []clause, name string) []string {
	var broken []string
	for _, c := range clauses {
		if c.breaks(name) {
			broken = append(broken, c.says)
		}
	}
	return broken
}
