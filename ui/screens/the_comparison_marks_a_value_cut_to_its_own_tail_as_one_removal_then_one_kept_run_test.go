package screens

// The comparison cuts the words both values begin alike and the words they end alike
// before it aligns anything, so the unchanged stretch a reader would call the same value
// is never walked twice. The cut at the end is clamped to the shorter side, and when the
// value a write left is wholly the tail of the value the record held — a person who
// deleted words from the front of a note — that clamp decides everything: whole of what
// the write left is the shared end, so the words it dropped come back as one removal and
// the words it left as one kept run. Written one word tighter (`tail < len(newWords)-head-1`)
// the shared end stops short, one word both values hold falls back into the middles, and
// the walk keeps it there while the rest of the record's words are struck out behind it:
// the panel then draws a word the write left as struck out in the middle of the line.
// Measured over every pair of values of up to five words built from three words — 132 496
// pairs, printed by a throwaway oracle over wordEdits — 528 of them answer differently
// under that one term, and no case in this package notices. This asks the cut.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonMarksAValueCutToItsOwnTailAsOneRemovalThenOneKeptRun compares the
// value the record held against one the write cut down to its last two words, and asks
// what the panel is owed: one removal holding the words the write dropped, then one kept
// run holding the words both values end alike. The runs are counted, not searched in
// joined text, so a comparison that walked the shared end again instead of cutting it
// answers as four runs where the reader is owed two.
func TestTheComparisonMarksAValueCutToItsOwnTailAsOneRemovalThenOneKeptRun(t *testing.T) {
	held := "quoted draft draft filed"
	left := "draft filed"
	edits := wordEdits(held, left)
	kinds := map[resource.EditKind]string{
		resource.EditKept: "kept", resource.EditInserted: "inserted", resource.EditRemoved: "dropped",
	}
	runs := func() string {
		var got []string
		for _, edit := range edits {
			got = append(got, kinds[edit.Kind]+"×"+strconv.Itoa(len(strings.Fields(edit.Text))))
		}
		return strings.Join(got, ", ")
	}
	if len(edits) != 2 || edits[0].Kind != resource.EditRemoved || edits[1].Kind != resource.EditKept {
		t.Errorf("a value cut to its own tail came back as %d runs of %s, where the reader is owed the words the "+
			"write dropped as one removal ahead of one kept run of the %q both values end alike: the shared end is "+
			"cut before the words are aligned, so no word the write left is drawn struck out",
			len(edits), runs(), left)
	}
	words := func(kind resource.EditKind) int {
		count := 0
		for _, edit := range edits {
			if edit.Kind == kind {
				count += len(strings.Fields(edit.Text))
			}
		}
		return count
	}
	if dropped, kept := words(resource.EditRemoved), words(resource.EditKept); dropped != 2 || kept != 2 {
		t.Errorf("the marks came back with %d dropped and %d kept words over the %d words the record held and the "+
			"%d the write left: every word of the value the record held is owed to the reader marked exactly once",
			dropped, kept, len(strings.Fields(held)), len(strings.Fields(left)))
	}
}
