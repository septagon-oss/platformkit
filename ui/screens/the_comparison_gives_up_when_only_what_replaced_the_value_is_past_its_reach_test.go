package screens

// The comparison gives up on aligning words when either side of a rewrite stands past the reach it keeps
// to itself, and the side that stands past it may be the one the write left rather than the one the record
// held. A field that held a sentence and now holds a written section is the ordinary shape of that, and
// the table the reach exists to bound is as wide as the longer side, so a guard that read only the value
// the record held would leave it open. A sentence cleared to several paragraphs arrives as one removal
// ahead of one insertion; the case beside it asks the mirror, a body cleared to a sentence.

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonGivesUpWhenOnlyWhatReplacedTheValueIsPastItsReach compares a short body the record held
// against a replacement well past the reach, the two holding every seventh word between them. Past the
// reach on the side the write left, the whole of what the record held has to arrive as one removal ahead of
// one insertion holding the whole of the replacement, and a word both bodies hold has to sit inside those
// two marks rather than come back as kept. The same two shapes with both sides inside the reach have to
// come back aligned and those shared words kept, which is what shows the answer differs by the reach and
// not by which side is longer.
func TestTheComparisonGivesUpWhenOnlyWhatReplacedTheValueIsPastItsReach(t *testing.T) {
	// body builds one side of a comparison: count words between two words both sides hold, every
	// seventh of them a word both bodies hold and the rest this side's own.
	body := func(count int, held bool) string {
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
	// runs answers, for one kind of mark, how many the comparison drew and how many words each holds.
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
		if len(parts) > 6 { // a comparison that never gives up draws dozens of runs; the shape that names them is the first few
			parts = append(parts[:6], "…")
		}
		return strings.Join(parts, " ")
	}
	holds := func(mark [][]string, word string) bool {
		return len(mark) > 0 && slices.Contains(mark[0], word)
	}
	first := func(edits []resource.VersionEdit, kind resource.EditKind) int {
		for i, e := range edits {
			if e.Kind == kind {
				return i
			}
		}
		return -1
	}

	// The record held a sentence and this write left several paragraphs: the long side is the one past
	// the reach, and a guard that asked only the value the record held would align this rewrite word for
	// word over a table as wide as the whole of what the write left.
	sentence, paragraphs := 30, comparedWords+40
	far := wordEdits(body(sentence, true), body(paragraphs, false))
	dropped, added := runs(far, resource.EditRemoved), runs(far, resource.EditInserted)
	if len(runs(far, resource.EditKept)) != 2 || len(dropped) != 1 || len(added) != 1 ||
		len(dropped[0]) != sentence || len(added[0]) != paragraphs {
		t.Fatalf("a sentence of %d words replaced by a body of %d drew %s: %d removals and %d insertions, "+
			"where one side past the reach of the comparison leaves the reader owed the whole of what the record "+
			"held as one removal and the whole of what replaced it as one insertion",
			sentence, paragraphs, shape(far), len(dropped), len(added))
	}
	// A word both bodies hold arrives inside the marks, not as kept: a comparison that abstained aligned
	// nothing and owes the reader no unchanged word.
	if !holds(dropped, "held3") || !holds(added, "held3") {
		t.Errorf("the rewrite past the reach on the side the write left left a word both bodies hold outside "+
			"its marks: %s, where a comparison that abstained holds every word of both bodies inside the mark "+
			"of its kind", shape(far))
	}
	if droppedAt, addedAt := first(far, resource.EditRemoved), first(far, resource.EditInserted); droppedAt > addedAt {
		t.Errorf("the comparison drew the mark of what this write left at %d before the mark of what the record "+
			"used to hold at %d: the reader meets the body that replaced the record's sentence before the "+
			"sentence itself: %s", addedAt, droppedAt, shape(far))
	}

	// Both sides inside the reach: the same two shapes, aligned, the shared words kept. A comparison that
	// never gives up fails here as surely as one that gives up too far in.
	near := wordEdits(body(sentence, true), body(comparedWords/2+1, false))
	if nearKept := runs(near, resource.EditKept); len(nearKept) <= 2 {
		t.Errorf("a sentence of %d words replaced by %d words, both sides inside the reach of the comparison, "+
			"was drawn as %s: only %d kept stretches, where the words both bodies hold are owed to the reader "+
			"as kept", sentence, comparedWords/2+1, shape(near), len(nearKept))
	}
}
