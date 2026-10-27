package screens

// A version of a body is paragraphs long, and the panel compares the whole of two of
// them word by word up to the reach of its alignment. Past that reach the two values
// are a rewrite rather than an edit, and the whole of what the write dropped and the
// whole of what it left still have to be marked: a panel that gave up on comparing a
// long body would show the person who reads it one plain stretch, with no sign of
// which words the write moved or what the record used to hold. Nothing in the tree
// hands the panel a body past that reach, so this case is the first to ask it.

import (
	"strings"
	"testing"
)

// TestTheServedPanelMarksABodyPastTheReachOfItsComparison compares two bodies longer
// than the panel aligns word by word, each holding a character the page draws as an
// entity, and reads the served line of the version that rewrote them. The line still
// has to mark what the write dropped and what it left, hold each body's words drawn
// once inside the mark of its kind, and read back as the body the version left
// standing — a rewrite the panel stayed silent about reads as a version that changed
// nothing.
func TestTheServedPanelMarksABodyPastTheReachOfItsComparison(t *testing.T) {
	// Past the alignment's reach on both sides, with no word shared at either end, and
	// the character the page has to draw in each body: the dropped words hold angle
	// brackets and the words this write left hold an ampersand.
	const tail = 80
	before := strings.Repeat("alpha bravo charlie ", tail) + "dropped <1>"
	after := strings.Repeat("mike november oscar ", tail) + "left &"
	line := comparedLine(t, before, after)

	dropped, added := markedRuns(t, line, "del"), markedRuns(t, line, "ins")
	if len(dropped) == 0 || len(added) == 0 {
		t.Fatalf("the line of a body this write rewrote holds %d dropped and %d added runs: the reader "+
			"is handed one stretch and no sign of what changed: %q", len(dropped), len(added), truncated(line))
	}
	// Every mark of every kind, joined: past the reach it is one run each, but the panel
	// is not asked to say how many, only that nothing of the two bodies is lost.
	droppedWords, addedWords := strings.Join(dropped, " "), strings.Join(added, " ")
	for _, side := range []struct{ words, foreign, of string }{
		{droppedWords, "mike", "dropped"}, {addedWords, "alpha", "added"},
	} {
		if strings.Contains(side.words, side.foreign) {
			t.Errorf("the %s words of the compared line hold %q, a word of the body on the other side of "+
				"the rewrite: %q", side.of, side.foreign, truncated(side.words))
		}
	}
	// Each body's own characters reach the reader drawn once, inside the mark that says
	// what became of them.
	for _, drawn := range []struct{ entity, char, from string }{
		{"&amp;", "&", after}, {"&lt;", "<", before}, {"&gt;", ">", before},
	} {
		if got, want := strings.Count(line, drawn.entity), strings.Count(drawn.from, drawn.char); got != want {
			t.Errorf("the compared line holds %d drawn %q for the %d the %q body wrote: %q",
				got, drawn.entity, want, truncated(drawn.from), line)
		}
	}
	for _, twice := range []string{"&amp;amp;", "&amp;lt;", "&amp;gt;"} {
		if strings.Contains(line, twice) {
			t.Errorf("the compared line drew %s, so the page escaped what was already drawn", twice)
		}
	}
	if held := readBack(line); held != after {
		t.Errorf("the compared line reads back as %q of a body that held %q: the reader is handed letters "+
			"the record does not hold, or loses letters it does", truncated(held), truncated(after))
	}
}

// truncated is the head of a long body, so a failure names the shape it met without
// printing two paragraphs into the test log.
func truncated(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}
