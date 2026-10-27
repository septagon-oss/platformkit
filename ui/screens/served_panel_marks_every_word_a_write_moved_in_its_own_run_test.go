package screens

// The cases beside this one compare two bodies that moved one clause, so each kind of
// mark appears once in the served line and reading the first mark of a kind reads the
// line. A write that moved two separate clauses marks each of them, and it merges the
// words of one clause into one mark rather than wearing an element per word — the pair
// of promises a single mark of each kind cannot tell apart, because it asks neither how
// many marks the write made nor what the later ones hold. This case moves two clauses,
// one dropped carrying an ampersand and one carrying angle brackets, and reads every
// mark: how many of each kind the write made and in what order, each holding its own
// words drawn once, nothing escaped twice, and the whole reading back as the body the
// version left standing.

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheServedPanelMarksEveryWordAWriteMovedInItsOwnRun compares two bodies that move
// two clauses apiece and reads every marked run of the served line of the version that
// moved them. The write has to stand as two dropped runs and two added runs in the order
// they read, each holding its own words drawn once, and the line read back through one
// unescaping has to be the body the version left standing.
func TestTheServedPanelMarksEveryWordAWriteMovedInItsOwnRun(t *testing.T) {
	// Two clauses dropped and two added, and the characters the page has to draw sit in
	// different marks: the dropped words hold an ampersand and angle brackets, and the
	// words the write added hold an ampersand in the later clause only. Far shorter than
	// one line of this panel, so nothing is cut.
	const before, after = "Sold & kept two <3> items", "Sold plus more kept & items"
	line := comparedLine(t, before, after)

	dropped, added := markedRuns(t, line, "del"), markedRuns(t, line, "ins")
	if len(dropped) != 2 || len(added) != 2 {
		t.Fatalf("the line %q marks the write as %d dropped and %d added runs of a body that moved "+
			"two clauses each: a clause spread over a mark per word reads to the person who hears "+
			"this line as a stutter, and two clauses merged read as one write that moved one", line,
			len(dropped), len(added))
	}
	// Every mark of each kind, not the first one only: a drawing that escapes what the
	// first run holds and hands the rest raw leaves the reader the record's own
	// characters as the markup of the page.
	for i, want := range []string{"&amp; ", "two &lt;3&gt; "} {
		if dropped[i] != want {
			t.Errorf("mark %d of the dropped words holds %q rather than %q, the words this write "+
				"dropped drawn once: %q", i, dropped[i], want, line)
		}
	}
	for i, want := range []string{"plus more ", "&amp; "} {
		if added[i] != want {
			t.Errorf("mark %d of the added words holds %q rather than %q, the words this write "+
				"added drawn once: %q", i, added[i], want, line)
		}
	}
	for _, twice := range []string{"&amp;amp;", "&amp;lt;", "&amp;gt;"} {
		if strings.Contains(line, twice) {
			t.Errorf("the compared line drew %s, so the page escaped what was already drawn: %q", twice, line)
		}
	}
	if held := readBack(line); held != after {
		t.Errorf("the compared line reads back as %q of a body that held %q: the reader is handed "+
			"letters the record does not hold, or loses letters it does", held, after)
	}
}

// markedRuns is what the panel put inside every element that marks a run as added or
// dropped, in the order a reader meets them, and an empty slice where it marked no such
// run. The helper beside this one answers for the first mark of a kind only.
func markedRuns(t *testing.T, line, mark string) []string {
	t.Helper()
	pattern := "(?s)<" + mark + ">(.*?)</" + mark + ">"
	runs := regexp.MustCompile(pattern).FindAllStringSubmatch(line, -1)
	held := make([]string, 0, len(runs))
	for _, run := range runs {
		held = append(held, run[1])
	}
	return held
}
