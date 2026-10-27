package screens

// The comparison cuts the words both values begin alike and the words they end alike before it
// aligns anything, and each cut stops at the shorter side: the loop for the shared end is clamped by
// `tail < len(newWords)-head` and its twin by `tail < len(oldWords)-head`. Those two `-head` terms
// keep the cuts from eating into each other. Drop either one and the shared end reaches behind the
// shared beginning, the middle slice `words[head:len(words)-tail]` is reversed, and the panel beside
// a record is a panic rather than a page. The shape that reaches it is ordinary: a value wholly the
// beginning of the other, which happens when a person types the same word twice — "draft" becomes
// "draft draft" — or deletes all but the words a note begins with.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonMarksAValueThatIsWhollyTheBeginningOfTheOther compares a value against one that
// holds it whole at the beginning and asks the marks the reader is owed: the words both values begin
// alike as one kept run, then the words only one of them holds as one removal or one insertion. The
// runs are counted, so a comparison whose two cuts overlap answers as a panic or as extra runs where
// the reader is owed two.
func TestTheComparisonMarksAValueThatIsWhollyTheBeginningOfTheOther(t *testing.T) {
	// Each row is the value the record held, the value the write left, and the marks the reader is
	// owed after the kept run: what the write dropped, or what it added.
	for _, row := range []struct {
		held, left, second string
		kept, rest         int
	}{
		{"draft", "draft draft", "inserted", 1, 1},
		{"draft filed", "draft filed filed", "inserted", 2, 1},
		{"draft filed", "draft filed quoted filed", "inserted", 2, 2},
		{"draft draft draft", "draft", "dropped", 1, 2},
		{"draft filed filed", "draft", "dropped", 1, 2},
	} {
		edits := wordEdits(row.held, row.left)
		shape := func() string {
			kinds := map[resource.EditKind]string{
				resource.EditKept: "kept", resource.EditInserted: "inserted", resource.EditRemoved: "dropped",
			}
			var got []string
			for _, edit := range edits {
				got = append(got, kinds[edit.Kind]+"×"+strconv.Itoa(len(strings.Fields(edit.Text))))
			}
			return strings.Join(got, ", ")
		}
		want := "kept×" + strconv.Itoa(row.kept) + ", " + row.second + "×" + strconv.Itoa(row.rest)
		if got := shape(); got != want {
			t.Errorf("%q against %q came back as %s, where the reader is owed %s: the words both values "+
				"begin alike are one kept run and the words only one value holds are one run after it, "+
				"because each cut of the shared stretch stops at the shorter side and the two cuts cannot "+
				"overlap", row.held, row.left, got, want)
			continue
		}
		heldWords, leftWords := len(strings.Fields(row.held)), len(strings.Fields(row.left))
		marked := func(kinds ...resource.EditKind) int {
			count := 0
			for _, edit := range edits {
				for _, kind := range kinds {
					if edit.Kind == kind {
						count += len(strings.Fields(edit.Text))
					}
				}
			}
			return count
		}
		if ofOld := marked(resource.EditKept, resource.EditRemoved); ofOld != heldWords {
			t.Errorf("the marks over %q cover %d words where the value the record held has %d: every word "+
				"of it is owed to the reader marked exactly once", row.held, ofOld, heldWords)
		}
		if ofNew := marked(resource.EditKept, resource.EditInserted); ofNew != leftWords {
			t.Errorf("the marks cover %d words of the value the write left where it holds %d: every word of "+
				"it is owed to the reader marked exactly once", ofNew, leftWords)
		}
	}
}
