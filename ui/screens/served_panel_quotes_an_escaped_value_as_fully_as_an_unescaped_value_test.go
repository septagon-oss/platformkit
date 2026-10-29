package screens

// The panel quotes one line of what a version held, so the length of that line is a
// statement about the record: a value quoted short says the record holds more, and a
// value quoted whole says it does not. Which letters fit the line is therefore measured
// against the record, never against the page the record is drawn on. The cases beside
// this file take values deliberately free of anything the markup would escape, so none of
// them asks the question a body full of "&" and "<" poses: a line measured after the
// value is drawn eats several of its letters for every character the page escapes, and
// can stop inside half an entity, so the quote comes back shorter than the record — the
// panel would say a version held fewer words than it held. This asks the question of two
// values of one length, differing only in whether the page has to escape them.

import (
	"html"
	"strings"
	"testing"
)

// TestTheServedPanelQuotesAnEscapedValueAsFullyAsAnUnescapedValue quotes two long values
// of the same letter count — one of plain letters, one with the letters a page draws as
// entities — and requires both lines to hold the same number of the value's own letters,
// each of them the stretch the version held from its first letter.
func TestTheServedPanelQuotesAnEscapedValueAsFullyAsAnUnescapedValue(t *testing.T) {
	// Long enough that no line of this panel holds either value, so both are quoted short.
	plain := strings.Repeat("alpha bravo charlie ", 30)
	// The same letters, in the same places, with two of them exchanged for characters the
	// page escapes: one length by construction, differing only in how they are drawn.
	escapes := strings.NewReplacer("a", "&", "o", "<").Replace(plain)

	// letters is what one line of the panel says the version held, in the value's own
	// letters rather than in the markup drawn around them.
	letters := func(value string) string {
		line := quotedLine(t, value)
		if !strings.HasSuffix(line, "…") {
			t.Fatalf("a value of %d letters was quoted with no sign that it goes past the line: %q",
				len([]rune(value)), line)
		}
		held := html.UnescapeString(strings.TrimSuffix(line, "…"))
		if !strings.HasPrefix(value, held) {
			t.Fatalf("the line holds %q, which is no stretch of what the version held", held)
		}
		return held
	}

	whole := letters(plain)
	marked := letters(escapes)
	if len([]rune(marked)) != len([]rune(whole)) {
		t.Errorf("the panel quoted %d letters of a value it draws as it reads them and %d letters of one "+
			"it escapes, though both values hold %d: the line was measured against the markup, not the record",
			len([]rune(whole)), len([]rune(marked)), len([]rune(plain)))
	}
}
