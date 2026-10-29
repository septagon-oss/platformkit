package screens

// The comparison bounds each side of a rewrite on its own: it gives up when either the
// words the record held or the words the write left are past `comparedWords`, and the
// guard reads `>` on both, so each side has its own bound and each bound is one a body
// may stand exactly on. The cases in this package put the long body on the side the
// record held — including the one whose fixture is a sentence on the other side, which
// writes its long side forty words past the reach — so the bound of the side that
// replaced the value stands on nothing: measured with that one term written a word
// looser (`len(newMid) > comparedWords+1`), every case here passes while a write that
// replaced a word with 201 words is drawn as an edited field rather than as the
// rewrite it is. This case asks that bound, on the boundary and one word past it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonAlignsAReplacementAtItsReachAndGivesUpOneWordPastIt compares a
// one-word value against a replacement of exactly the reach, then of one word more. At
// the reach the word both sides hold has to come back as kept, which an abstaining
// comparison holds inside its marks instead; one word past it the whole of what the
// write left has to arrive as one insertion behind the one removal holding the whole of
// what the record held. Both replacements are written of the reach itself, so the pair
// sits one word apart on either side of whatever reach the comparison declares.
func TestTheComparisonAlignsAReplacementAtItsReachAndGivesUpOneWordPastIt(t *testing.T) {
	// replacement writes count words between two words both sides hold, every seventh one
	// a word the short side holds too and the rest this write's own family.
	replacement := func(count int, own string) string {
		out := []string{"filed"}
		for i := 0; i < count; i++ {
			if i%7 == 3 {
				out = append(out, "shared"+strconv.Itoa(i))
			} else {
				out = append(out, own+strconv.Itoa(i))
			}
		}
		return strings.Join(append(out, "signed"), " ")
	}
	counts := func(edits []resource.VersionEdit) (kept, dropped, added int) {
		for _, edit := range edits {
			switch edit.Kind {
			case resource.EditKept:
				kept++
			case resource.EditRemoved:
				dropped++
			case resource.EditInserted:
				added++
			}
		}
		return kept, dropped, added
	}
	held := "filed shared3 made0 signed"
	if kept, _, _ := counts(wordEdits(held, replacement(comparedWords, "made"))); kept < 3 {
		t.Errorf("a replacement of exactly the reach of %d words over one well inside it came back with %d kept "+
			"stretches: the reach bounds what a write left as surely as what the record held, so the word both "+
			"bodies hold is owed to the reader as kept, and only a comparison that abstained holds it inside one "+
			"removal of the whole value ahead of one insertion of the whole of what replaced it", comparedWords, kept)
	}
	if kept, dropped, added := counts(wordEdits(held, replacement(comparedWords+1, "made"))); kept != 2 || dropped != 1 || added != 1 {
		t.Errorf("a replacement of one word past the reach of %d words came back as kept×%d dropped×%d added×%d, "+
			"where the reader is owed the whole of what the record held as one removal ahead of one insertion "+
			"holding the whole of the %d words that replaced it", comparedWords, kept, dropped, added, comparedWords+1)
	}
}
