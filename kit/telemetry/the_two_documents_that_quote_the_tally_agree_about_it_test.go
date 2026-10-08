package telemetry_test

// The one number this delivery publishes, quoted in two documents.
//
// The brief asks the commit to state `tenant_attributed_boundary_coverage` as a
// ratio, and the delivery states it twice: `CHANGELOG.md` gives "80 of the 81
// boundaries the reference application registers as operations and module
// declarations — 69 operations, 5 module jobs, 7 subscriptions", and this guide
// repeats "80 of the 81 operation and module-declared boundaries". Both sentences
// are this branch's own writing, and the brief's rule is that the branch which
// changes a figure updates the quote in the same change.
//
// This class has rotted twice already: one note's first sentence implied
// `file-reconcile` was the only boundary carrying no tenant, and
// `daed86a` found the guide calling the excluded four "relay and purge jobs" while
// the note named a migration drain among them — two documents disagreeing about the
// same four jobs. A case with a regex over the prose would tax prose, so nothing
// below reads a sentence's wording. It reads only the figures, only out of these two
// files, so no other branch's merge can move them: the note's entry is this task's
// own, and no other task edits it. Both cases pass today; each goes red on a figure
// edited out of step with the others, which is the failure this branch has produced
// twice, and neither depends on the tree, on the base, or on a job list another task
// may lengthen.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The two shapes the documents print. The pair may be wrapped across a line
// (CHANGELOG.md breaks after "the 81") and may carry words before the noun (the
// guide says "81 operation and module-declared boundaries"), so what separates the
// denominator from the noun is whatever the prose put there, up to a clause.
var (
	pairPattern  = regexp.MustCompile(`(?s)(\d+)\s+of\s+the\s+(\d+)[^.]{0,60}?boundar`)
	termsPattern = regexp.MustCompile(`(?s)(\d+)\s+operations?,\s*(\d+)\s+[^,]{0,20}jobs?,\s*(\d+)\s+subscriptions?`)
)

// tally is one published ratio and the terms its author claims it is made of.
type tally struct {
	covered, total int
	terms          []int
}

// publishedTally reads the ratio out of a document. A document that stops printing
// either shape is refused rather than skipped: a silent skip is how a retired quote
// passes review. Only the note promises a decomposition; the guide quotes the ratio
// and points at the note for it, so the guide is read for the pair alone.
func publishedTally(t *testing.T, body, what string) tally {
	t.Helper()
	at := pairPattern.FindStringSubmatchIndex(body)
	if at == nil {
		t.Fatalf("%s no longer prints a boundary tally of the form \"80 of the 81 … boundaries\"", what)
	}
	pair := pairPattern.FindStringSubmatch(body)
	covered, err := strconv.Atoi(pair[1])
	if err != nil {
		t.Fatalf("%s: covered term %q is not a number: %v", what, pair[1], err)
	}
	total, err := strconv.Atoi(pair[2])
	if err != nil {
		t.Fatalf("%s: denominator %q is not a number: %v", what, pair[2], err)
	}
	start, stop := at[0], at[1]
	clause := body[max(start-400, 0):min(stop+400, len(body))]
	terms := termsPattern.FindStringSubmatch(clause)
	if terms == nil {
		if what == "the release note" {
			t.Fatalf("%s prints %d of %d with no decomposition beside it: the note's own gloss is what makes "+
				"its denominator checkable", what, covered, total)
		}
		return tally{covered: covered, total: total}
	}
	out := make([]int, 0, 3)
	for _, s := range terms[1:] {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("%s: term %q is not a number: %v", what, s, err)
		}
		out = append(out, n)
	}
	return tally{covered: covered, total: total, terms: out}
}

func readBothDocuments(t *testing.T) (note, guide string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read the release note: %v", err)
	}
	g, err := os.ReadFile(filepath.Join("..", "..", "kit", "telemetry", "README.md"))
	if err != nil {
		t.Fatalf("read the package guide: %v", err)
	}
	return string(b), string(g)
}

// TestThePublishedTallyAddsUp: the note says its 81 boundaries are "69 operations,
// 5 module jobs, 7 subscriptions". Three terms that do not sum to the fourth are a
// published figure contradicting its own gloss.
func TestThePublishedTallyAddsUp(t *testing.T) {
	note, _ := readBothDocuments(t)
	got := publishedTally(t, note, "the release note")
	if len(got.terms) < 2 {
		t.Fatalf("the release note's tally decomposes into %d terms (%v), which is not a decomposition",
			len(got.terms), got.terms)
	}
	sum := 0
	for _, n := range got.terms {
		sum += n
	}
	if sum != got.total {
		t.Errorf("the release note publishes %d of %d boundaries and decomposes the %d into %v, which sum to %d: "+
			"a published ratio and its own terms must name one and the same denominator",
			got.covered, got.total, got.total, got.terms, sum)
	}
	if got.covered > got.total {
		t.Errorf("the release note claims %d of %d boundaries carry a tenant — more than it says it registered",
			got.covered, got.total)
	}
}

// TestTheGuideAndTheNoteQuoteOneTally: the guide points at the note ("named as
// excluded in `CHANGELOG.md`") for the same figure. Two documents quoting two
// different ratios for one measurement is the disagreement `daed86a` was written to
// cure, and nothing in the tree keeps the two in step today.
func TestTheGuideAndTheNoteQuoteOneTally(t *testing.T) {
	note, guide := readBothDocuments(t)
	fromNote := publishedTally(t, note, "the release note")
	fromGuide := publishedTally(t, guide, "the package guide")
	if fromNote.covered != fromGuide.covered || fromNote.total != fromGuide.total {
		t.Errorf("the guide quotes %d of %d boundaries where the release note quotes %d of %d: the guide says "+
			"the note names its exclusions, and one measurement cannot have two ratios",
			fromGuide.covered, fromGuide.total, fromNote.covered, fromNote.total)
	}
	if !strings.Contains(guide, "CHANGELOG.md") {
		t.Errorf("the guide's ratio sentence no longer points at the release note it quotes, so a reader cannot "+
			"tell which tally it means: %d of %d", fromGuide.covered, fromGuide.total)
	}
}
