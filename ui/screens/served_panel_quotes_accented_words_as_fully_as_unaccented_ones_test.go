package screens

// The panel quotes a stored value in the language the page speaks, and the words of that
// language hold runes of two bytes: "versão", "análise", "coração". A line cut at a count
// of bytes instead of a count of letters gives the tenant's language a shorter line than
// the same page draws in English, and can stop half way through a letter. Every value the
// panel's other cases hold is plain ASCII, where those two counts are the same number, so
// none of them tells them apart. This case asks the same panel for a line of unaccented
// words and a line of accented ones of the same length, and requires each to hold as many
// letters as the other. The length a line reaches is read off the served line itself, so
// no figure is pinned that the panel does not own.

import (
	"strings"
	"testing"
)

// TestTheServedPanelQuotesAccentedWordsAsFullyAsUnaccentedOnes gives the panel two values
// of one rune length and one word rhythm, one carrying accents and one not, and requires
// the line drawn for each to quote as many letters as the other. A cut measured in bytes
// quotes about half as many of the accented words, because those letters weigh more.
func TestTheServedPanelQuotesAccentedWordsAsFullyAsUnaccentedOnes(t *testing.T) {
	// Seven runes a cycle in both spellings; "versão " weighs nine bytes to "pessoa "'s seven.
	const cycles = 60
	plainValue := strings.Repeat("pessoa ", cycles)
	accentedValue := strings.Repeat("versão ", cycles)
	plain := quotedLine(t, plainValue)
	accented := quotedLine(t, accentedValue)
	if !strings.HasSuffix(plain, "…") || !strings.HasSuffix(accented, "…") {
		t.Fatalf("a value of %d runes was quoted with no sign that it goes past the line, so neither line is a cut: unaccented %q, accented %q",
			len([]rune(plainValue)), plain, accented)
	}
	keptPlain := strings.TrimSuffix(plain, "…")
	keptAccented := strings.TrimSuffix(accented, "…")
	if !strings.HasPrefix(accentedValue, keptAccented) {
		t.Errorf("the line for the accented value holds %q, which is not a stretch of what the version held", keptAccented)
	}
	if len([]rune(keptPlain)) != len([]rune(keptAccented)) {
		t.Errorf("the panel holds %d letters of accented words and %d of unaccented ones of the same length: the line was measured in bytes, not letters",
			len([]rune(keptAccented)), len([]rune(keptPlain)))
	}
}
