package screens

// The comparison aligns a rewrite whose longer side holds up to `comparedWords` words and gives
// up one word past that, so the reach is a bound a body may stand exactly on. Nothing stands on
// it elsewhere in the package: the cases beside this file write their long side forty words past
// the reach and their short side at half of it, so a comparison that gave up *at* the reach
// rather than past it would answer every one of them the line they already expect. Both bodies
// here are written of the reach itself, so this answers the boundary and nothing else, and a
// reach moved to any size leaves the two bodies one word apart on either side of it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheComparisonAlignsABodyAtItsReachAndGivesUpOneWordPastIt compares a body of exactly the
// reach, then one of a word more, against a short side that holds one word the body holds. At the
// reach the word both sides hold has to come back as kept, which an abstaining comparison holds
// inside its marks instead; one word past it the whole body has to arrive as one removal ahead of
// one insertion, with no word kept between the two both sides begin and end with.
func TestTheComparisonAlignsABodyAtItsReachAndGivesUpOneWordPastIt(t *testing.T) {
	// body writes count words between two words both sides hold, every seventh one a word
	// the short side holds too and the rest this side's own family.
	body := func(count int, own string) string {
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
	short := "filed shared3 made0 signed"
	if kept, _, _ := counts(wordEdits(body(comparedWords, "held"), short)); kept < 3 {
		t.Errorf("a body of exactly the reach of %d words against one well inside it came back with %d kept "+
			"stretches: the reach is a bound the body may stand on, so the word both bodies hold is owed to the "+
			"reader as kept, and only a comparison that abstained holds it inside one removal of the whole body "+
			"ahead of one insertion of the whole of what replaced it", comparedWords, kept)
	}
	if kept, dropped, added := counts(wordEdits(body(comparedWords+1, "held"), short)); kept != 2 || dropped != 1 || added != 1 {
		t.Errorf("a body of one word past the reach of %d words came back as kept×%d dropped×%d added×%d, where "+
			"the reader is owed the whole of the body the record held as one removal ahead of one insertion "+
			"holding the whole of what replaced it", comparedWords, kept, dropped, added)
	}
}
