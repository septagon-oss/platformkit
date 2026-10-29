package screens

// The panel's line says in one sign that a value went past what it quotes, and the
// sign has to answer for the whole line: the line owes a reader every word it could
// have held. A value cut shorter than the line reaches loses words for nothing, and
// says nothing about it — the line still ends in the sign and reads as though it
// filled. Where the sign belongs, and that a quoted value quoted again comes back
// whole, are both pinned beside this file; neither sees an over-short cut, because
// both read the line the page drew rather than the longest line it could draw.
//
// The length a line reaches is read off the page instead of written here, so this
// case travels with the panel's own line length and pins no figure no other file owns.

import (
	"strings"
	"testing"
)

// TestTheServedPanelKeepsEveryWordTheLineCouldHaveHeld asks the panel for the stretch
// of a long value it quotes, then hands that stretch back with one rune added, and
// requires the served line to be the stretch it was given plus the sign: the cut gave
// up only the words that would not fit. A line cut short of the length it may reach
// answers the second call with fewer runes than it was offered, or with no sign at all.
func TestTheServedPanelKeepsEveryWordTheLineCouldHaveHeld(t *testing.T) {
	long := strings.Repeat("alpha bravo charlie delta ", 12)
	line := quotedLine(t, long)
	if !strings.HasSuffix(line, "…") {
		t.Fatalf("a value %d runes long drew no sign that it goes past the line: %q", len([]rune(long)), line)
	}
	kept := strings.TrimSuffix(line, "…")
	// One rune more than the line kept, so it is the cut being asked about and not the
	// length; "z" because a value the markup would escape is not the value read back.
	if again := quotedLine(t, kept+"z"); again != kept+"…" {
		t.Errorf("the line kept %d runes of a value offering %d and answered %q, want every rune the line could hold and then the sign",
			len([]rune(kept)), len([]rune(kept))+1, again)
	}
}
