package screens

// The panel marks which words a write added and which it dropped, and a person who typed
// an "&" or a "<" can put one of them inside either kind of mark. The cases beside this
// one hand each mark a different character: a dropped word holds an ampersand, an added
// one holds angle brackets. Drawn once, those characters are the record's own; drawn raw
// they become the reader's markup, and drawn twice they become the letters of their own
// drawing. Asking each mark separately is the point — a drawing that escapes only some of
// what a body holds satisfies a run built from one character and fails a run built from
// another, and a line where the dropped word and the added word hold the same character
// cannot tell those two drawings apart at all.

import (
	"strings"
	"testing"
)

// TestTheServedPanelDrawsEachMarkedWordAsTheRecordWroteIt compares two bodies whose
// dropped word holds an ampersand and whose added word holds angle brackets, and reads
// the served line of the version that moved them. Each character either body wrote has to
// reach that line drawn once, each mark has to hold the drawn form of the word inside it,
// and the line read back through one unescaping has to be the body the version left
// standing.
func TestTheServedPanelDrawsEachMarkedWordAsTheRecordWroteIt(t *testing.T) {
	// One ampersand, in the word this write dropped; one pair of angle brackets, in the
	// word it added. Far shorter than one line of this panel, so nothing is cut.
	const before, after = "Sold & kept", "Sold <new>"
	line := comparedLine(t, before, after)

	for _, drawn := range []struct{ entity, char string }{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"},
	} {
		want := strings.Count(before, drawn.char) + strings.Count(after, drawn.char)
		if got := strings.Count(line, drawn.entity); got != want {
			t.Errorf("the compared line holds %d drawn %q for the %d the two bodies wrote: %q",
				got, drawn.entity, want, line)
		}
	}
	for _, twice := range []string{"&amp;amp;", "&amp;lt;", "&amp;gt;"} {
		if strings.Contains(line, twice) {
			t.Errorf("the compared line drew %s, so the page escaped what was already drawn: %q", twice, line)
		}
	}
	// Each mark carries its own character: the dropped word's ampersand and the added
	// word's brackets, each drawn. A mark holding raw text places the record's characters
	// in the markup of a page another person reads.
	if dropped := markedRun(line, "del"); !strings.Contains(dropped, "&amp;") {
		t.Errorf("the mark of a dropped word holds %q rather than the drawn ampersand the body "+
			"held, so the reader is handed markup: %q", dropped, line)
	}
	if added := markedRun(line, "ins"); !strings.Contains(added, "&lt;new&gt;") {
		t.Errorf("the mark of an added word holds %q rather than the drawn brackets the body wrote, "+
			"so the reader is handed markup: %q", added, line)
	}
	if held := readBack(line); held != after {
		t.Errorf("the compared line reads back as %q of a body that held %q: the reader is handed "+
			"letters the record does not hold, or loses letters it does", held, after)
	}
}
