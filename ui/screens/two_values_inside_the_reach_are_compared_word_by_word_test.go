package screens

// The comparison gives up on a rewrite wider than its reach, and the reach is its own, so a
// case that shows a reader two values small enough to be aligned has to hold both sides under
// that reach. Thirty words is the shorter side the reach cases are written at: a reach below it
// makes those fixtures abstain, and a case whose fixture abstained would report that the guard
// failed where the fixture moved. This asks the pair at the reach standing in the package, so a
// reach moved either way answers here rather than in a case that blames the wrong thing.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTwoValuesInsideTheReachAreComparedWordByWord aligns a body of thirty words against one of
// half the reach plus a word and asks for the shared words back as kept. Both sides sit inside a
// reach of at least thirty words, which is what makes the answer the comparison's rather than the
// bound's; below that reach the pair is not inside it and the case says so by name.
func TestTwoValuesInsideTheReachAreComparedWordByWord(t *testing.T) {
	short, long := 30, comparedWords/2+1
	if short > comparedWords {
		t.Fatalf("the comparison abstains past %d words, and the pair this file aligns is written %d words on its "+
			"shorter side: the pair stands outside the reach, so what reads as a comparison is the bound",
			comparedWords, short)
	}
	// body writes count words that begin and end alike, every seventh one shared and the rest
	// this side's own family, so only the shared words can line the two bodies up.
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
	kept := 0
	for _, edit := range wordEdits(body(short, "held"), body(long, "made")) {
		if edit.Kind == resource.EditKept {
			kept++
		}
	}
	if kept <= 2 {
		t.Errorf("a body of %d words against one of %d, both inside the reach of %d, came back with %d kept "+
			"stretches: a comparison that abstained holds the words both bodies share inside its marks",
			short, long, comparedWords, kept)
	}
}
