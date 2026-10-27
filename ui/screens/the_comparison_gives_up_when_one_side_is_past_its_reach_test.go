package screens

// The comparison stops aligning words when either side of a rewrite is past the reach it keeps to
// itself, and one long side alone is enough to stop it: a note of several paragraphs cleared to one
// sentence is a rewrite, and the reader is owed the whole of what the record held as one removal,
// ahead of one insertion holding the sentence that replaced it. Every case that reaches that reach
// today builds bodies of the same length on both sides, so a comparison that abstained only when
// both sides stood past it would draw each of them the same line. Both fixtures are built from the
// reach, so moving the reach moves the bodies with them and this case keeps asking one question.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonGivesUpWhenOneSideIsPastItsReach compares a body well past the reach against a
// replacement well inside it, where the two hold every seventh word in common. Past the reach on one
// side the whole of the body the record held has to arrive as one removal, ahead of one insertion
// holding the whole of the replacement, and the words both bodies hold have to sit inside those two
// marks rather than come back as kept: having abstained, the comparison aligned nothing and owes the
// reader no unchanged word. The same pair of shapes with both sides inside the reach has to come back
// aligned, those common words kept, which is what shows the two answers differ by the reach and not
// by the length of one side.
func TestTheComparisonGivesUpWhenOneSideIsPastItsReach(t *testing.T) {
	// rewrite builds one side of a comparison: count words between two words both sides
	// hold, every seventh of them a word both bodies hold and the rest this side's own.
	rewrite := func(count int, held bool) string {
		out := []string{"filed"}
		for i := 0; i < count; i++ {
			switch {
			case i%7 == 3:
				out = append(out, "held"+strconv.Itoa(i))
			case held:
				out = append(out, "gone"+strconv.Itoa(i))
			default:
				out = append(out, "made"+strconv.Itoa(i))
			}
		}
		return strings.Join(append(out, "signed"), " ")
	}
	// runs answers, for one kind of mark, how many the comparison drew and how many words
	// each holds; shape answers how the marks stand in the order a reader meets them.
	runs := func(edits []resource.VersionEdit, kind resource.EditKind) [][]string {
		var out [][]string
		for _, e := range edits {
			if e.Kind == kind {
				out = append(out, strings.Fields(e.Text))
			}
		}
		return out
	}
	shape := func(edits []resource.VersionEdit) string {
		kinds := map[resource.EditKind]string{
			resource.EditKept: "kept", resource.EditInserted: "added", resource.EditRemoved: "dropped",
		}
		parts := make([]string, 0, len(edits))
		for _, e := range edits {
			parts = append(parts, kinds[e.Kind]+"×"+strconv.Itoa(len(strings.Fields(e.Text))))
		}
		return strings.Join(parts, " ")
	}
	one := func(edits []resource.VersionEdit, kind resource.EditKind) int {
		for i, e := range edits {
			if e.Kind == kind {
				return i
			}
		}
		return -1
	}
	holds := func(mark [][]string, word string) bool {
		return len(mark) > 0 && strings.Contains(strings.Join(mark[0], " "), word)
	}

	// One side past the reach, one side well inside it: the record held a body of several
	// paragraphs and this write left a sentence, which is the shape that asks the guard to
	// read both sides and abstain on the strength of either.
	long, short := comparedWords+40, 30
	far := wordEdits(rewrite(long, true), rewrite(short, false))
	dropped, added := runs(far, resource.EditRemoved), runs(far, resource.EditInserted)
	if len(runs(far, resource.EditKept)) != 2 || len(dropped) != 1 || len(added) != 1 ||
		len(dropped[0]) != long || len(added[0]) != short {
		t.Fatalf("a body of %d words replaced by a sentence of %d drew %s: %d removals and %d "+
			"insertions, where one side past the reach of the comparison leaves the reader owed the "+
			"whole body the record held as one removal and the whole of what replaced it as one "+
			"insertion", long, short, shape(far), len(dropped), len(added))
	}
	// The words both bodies hold arrive inside the marks, not as kept: a comparison that
	// aligned them would report them unchanged, and the reader would meet a line that
	// claims most of a rewritten body stood where it was.
	if !holds(dropped, "held3") || !holds(added, "held3") {
		t.Errorf("the rewrite past the reach on one side left a word both bodies hold outside its "+
			"marks: %s, where a comparison that abstained on the strength of one long side holds "+
			"every word of both bodies inside the mark of its kind", shape(far))
	}
	if droppedAt, addedAt := one(far, resource.EditRemoved), one(far, resource.EditInserted); droppedAt > addedAt {
		t.Errorf("the comparison drew the mark of what this write left at %d before the mark of what "+
			"the record used to hold at %d: the reader meets the sentence that replaced the body "+
			"before the body itself: %s", addedAt, droppedAt, shape(far))
	}

	// Both sides inside the reach: the same two shapes, aligned, with the words both bodies
	// hold back as kept. A comparison that gave up too far in fails here as surely as one
	// that never gives up fails above.
	near := wordEdits(rewrite(comparedWords/2+1, true), rewrite(short, false))
	if nearKept := runs(near, resource.EditKept); len(nearKept) <= 2 {
		t.Errorf("a rewrite of %d words over one of %d, both sides inside the reach of the "+
			"comparison, was drawn as %s: only %d kept stretches, where the words both bodies hold "+
			"are owed to the reader as kept", comparedWords/2+1, short, shape(near), len(nearKept))
	}
}
