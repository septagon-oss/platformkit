package screens

// The comparison stops aligning words past a reach it keeps to itself, and it stops without
// leaving a trace on the page: for two bodies that share no word the served line reads the
// same whether the comparison abstained or walked every word, because a run of one kind is
// one element however many words made it. The promise of that reach — that a body rewritten
// past it arrives as one removal and one insertion rather than as a mark per word — is asked
// here, of the comparison itself. Both fixtures are built from the reach the comparison
// declares, so moving that reach moves the bodies with it and this case keeps asking one.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestComparedWordsPastTheReachMarkOneRemovalAndOneInsertion compares two bodies whose
// rewrite is past the reach at which words are aligned, and again well inside it. Past it,
// the whole of what the write dropped has to arrive as one removal, ahead of one insertion
// holding the whole of what it left, with nothing from inside the rewrite drawn as kept;
// inside it, the words both bodies hold do have to come back as kept, which is what shows the
// two answers differ by the reach and not by the shape of the two bodies.
func TestComparedWordsPastTheReachMarkOneRemovalAndOneInsertion(t *testing.T) {
	// "filed" and "signed" both bodies hold, one at either end, so the comparison cuts them as
	// unchanged and what is left to align is the rewrite alone — every seventh word of which
	// both bodies hold, and nothing else.
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
	// stretches answers, for one kind of mark, how many runs the comparison drew and how many
	// words each holds; shape answers how the whole of them stand in the order a reader meets
	// them, so a failure names the shape it met instead of printing two rewritten bodies.
	stretches := func(edits []resource.VersionEdit, kind resource.EditKind) [][]string {
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
		if len(parts) > 6 { // a broken comparison draws dozens of runs; the shape that names them is the first few
			parts = append(parts[:6], "…")
		}
		return strings.Join(parts, " ")
	}

	past := comparedWords + 40
	far := wordEdits(body(past, true), body(past, false))
	kept := stretches(far, resource.EditKept)
	dropped := stretches(far, resource.EditRemoved)
	added := stretches(far, resource.EditInserted)
	if len(kept) != 2 || len(dropped) != 1 || len(added) != 1 || len(dropped[0]) != past || len(added[0]) != past {
		t.Fatalf("a body of %d words rewritten past the reach of the comparison drew %s: %d kept stretches, %d "+
			"removals and %d insertions, where the reader is owed one removal of the whole body the record held "+
			"and then one insertion of the whole body that replaced it",
			past, shape(far), len(kept), len(dropped), len(added))
	}
	if kept[0][0] != "filed" || kept[1][0] != "signed" || dropped[0][0] != "gone0" || added[0][0] != "made0" {
		t.Errorf("the rewrite past the reach was drawn from the wrong words: it kept %q and %q and opened its "+
			"marks with %q and %q, where the words both bodies hold inside the rewrite belong to no mark and "+
			"every other word of it belongs to one: %s",
			kept[0][0], kept[1][0], dropped[0][0], added[0][0], shape(far))
	}
	droppedAt, addedAt := -1, -1
	for i, e := range far {
		if e.Kind == resource.EditRemoved && droppedAt < 0 {
			droppedAt = i
		}
		if e.Kind == resource.EditInserted && addedAt < 0 {
			addedAt = i
		}
	}
	if droppedAt > addedAt {
		t.Errorf("the comparison drew the mark of what this write left at %d before the mark of what the record "+
			"used to hold at %d: the reader meets the body that replaced the record's before the body the "+
			"record held: %s", addedAt, droppedAt, shape(far))
	}

	inside := comparedWords/2 + 1
	near := wordEdits(body(inside, true), body(inside, false))
	if nearKept := stretches(near, resource.EditKept); len(nearKept) <= 2 {
		t.Fatalf("a rewrite of %d words inside the reach of the comparison was drawn as %s: only %d kept "+
			"stretches, where the words both bodies hold inside the rewrite are owed to the reader as kept",
			inside, shape(near), len(nearKept))
	}
}
