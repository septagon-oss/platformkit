package screens

// One of the panel's promises is that quoting is stable: a value that is already the
// length of one quoted line comes back whole, with no sign that anything is missing.
// The case beside this file pins that promise, and pins it over plain ASCII words,
// where a count of letters and a count of bytes are the same number. The panel quotes
// the language a tenant is served in, and the words of that language weigh more than
// a byte apiece, so the promise the ASCII case pins is not the promise the page keeps:
// a line measured in bytes hands back a stretch whose bytes are the count and whose
// letters are fewer, and asking the panel for that stretch once more is answered with
// no sign at all. This asks the stable-quote question over Portuguese words.

import (
	"strings"
	"testing"
)

// TestTheServedPanelQuotesAnAccentedValueAsStablyAsItQuotesAnASCIIOne hands the panel
// the longest stretch of a long accented value that it quotes, asks for that stretch
// back with one letter added, and requires the line drawn for it to be the stretch it
// was given and then the sign: the second reading of a quotation loses nothing.
func TestTheServedPanelQuotesAnAccentedValueAsStablyAsItQuotesAnASCIIOne(t *testing.T) {
	// "versão " is seven letters and eight bytes, so the panel's line reaches fewer
	// letters of it than of a value of the same length without accents.
	value := strings.Repeat("versão ", 60)
	line := quotedLine(t, value)
	if !strings.HasSuffix(line, "…") {
		t.Fatalf("a value of %d letters drew no sign that it goes past the line: %q", len([]rune(value)), line)
	}
	kept := strings.TrimSuffix(line, "…")
	if !strings.HasPrefix(value, kept) {
		t.Fatalf("the line holds %q, which is no stretch of what the version held", kept)
	}
	// One letter more than the line kept, so it is the cut being asked about and not
	// the length; "z" because a value the markup would escape is not the value read back.
	if again := quotedLine(t, kept+"z"); again != kept+"…" {
		t.Errorf("the line kept %d letters of an accented value offering %d and answered %q, want every letter the line could hold and then the sign",
			len([]rune(kept)), len([]rune(kept))+1, again)
	}
}
