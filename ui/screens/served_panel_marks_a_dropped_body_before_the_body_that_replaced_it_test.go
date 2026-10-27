package screens

// The panel compares a body against the one the record used to hold, and past the reach at
// which it aligns words it marks the whole of what the write dropped as one run and the
// whole of what it left as one more. Which mark reaches the reader first is not a layout
// detail: the reading order of the line is the order of the two bodies, and every shorter
// comparison on this panel keeps the words the record held before the words that replaced
// them. A body past that reach is asked nothing of that order today — the case beside this
// one counts the marks, holds each body's own letters inside the mark of its kind and reads
// the line back as the body that stood, and all three of those hold just as well for a line
// that answers with the new body first. This case asks the order.

import (
	"strings"
	"testing"
)

// TestTheServedPanelMarksADroppedBodyBeforeTheBodyThatReplacedIt compares two bodies past
// the reach at which the panel aligns words at all and reads the served line of the version
// that rewrote them. The mark of what the write dropped has to open before the mark of what
// it left, and no word of the body the record used to hold may reach the reader once the
// body that replaced it has begun — a line that answers with the new body first reads as a
// version that changed nothing, whatever else it marks.
func TestTheServedPanelMarksADroppedBodyBeforeTheBodyThatReplacedIt(t *testing.T) {
	// Past the reach on both sides, with no word shared at either end, so the panel is
	// asked to abstain from aligning and left to say what changed in one mark of each kind.
	const tail = 80
	before := strings.Repeat("alpha bravo charlie ", tail) + "dropped <1>"
	after := strings.Repeat("mike november oscar ", tail) + "left &"
	line := comparedLine(t, before, after)
	// head is the start of a long line, so a failure names the shape it met without
	// printing two paragraphs of a body into the test log.
	head := func(s string) string {
		if r := []rune(s); len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return s
	}

	droppedAt, addedAt := strings.Index(line, "<del>"), strings.Index(line, "<ins>")
	if droppedAt < 0 || addedAt < 0 {
		t.Fatalf("the line of a body this write rewrote holds no mark of what changed (what was dropped "+
			"opens at %d, what was left at %d): the reader is handed one stretch: %q",
			droppedAt, addedAt, head(line))
	}
	if droppedAt > addedAt {
		t.Errorf("the compared line opens the mark of what this write left at %d and the mark of what the "+
			"record used to hold at %d: the reader meets the body that replaced the record's and is never "+
			"told what stood before it", addedAt, droppedAt)
	}
	// The same promise read from the words rather than from the markup: the last word of
	// the body the record used to hold belongs before the first word of the body that took
	// its place, however the two are drawn.
	lastHeld, firstLeft := strings.LastIndex(line, "dropped"), strings.Index(line, "mike")
	if lastHeld < 0 || firstLeft < 0 {
		t.Fatalf("the compared line holds neither both bodies nor neither: the last word the record held "+
			"at %d, the first this write left at %d: %q", lastHeld, firstLeft, head(line))
	}
	if lastHeld > firstLeft {
		t.Errorf("the compared line writes a word the record no longer holds at %d, past the first word "+
			"this write left at %d: the two bodies reach the reader in the order the record does not "+
			"hold them: %q", lastHeld, firstLeft, head(line))
	}
}
