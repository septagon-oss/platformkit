package screens

// The panel quotes what a version held inside the page that shows it, so a record whose
// body carries a character the page draws as an entity — "&", "<", a quotation mark —
// reaches the reader drawn, not as it was written. Drawing it is right; drawing it twice
// is not. An ampersand escaped a second time arrives as the entity for its own entity, so
// the line reads "&amp;" where the record wrote "&", and a reader is told the record holds
// letters it does not hold. No panel case reached that far: the values beside this one are
// chosen free of anything the markup escapes, and the one that takes an escaped value asks
// only how many of the record's letters the line holds, which a doubly-drawn value still
// answers correctly. This asks what the reader is handed: every character the record holds
// once and whole, and no sign of a cut where nothing was cut.

import (
	"html"
	"strings"
	"testing"
)

// TestTheServedPanelDrawsAValueThePageEscapesAsTheRecordWroteIt quotes a body short enough
// for one line and holds the characters a page draws as entities. The line must carry the
// record's characters exactly, each escaped character drawn once, and no sign of a cut.
func TestTheServedPanelDrawsAValueThePageEscapesAsTheRecordWroteIt(t *testing.T) {
	// Far shorter than one line of this panel, so the whole body is quoted; and every
	// character in it that the page has to draw as an entity is in it twice at least.
	value := "Tom & Jerry <3> \"hearts\" for 'bob' & co"
	line := quotedLine(t, value)

	if strings.Contains(line, "…") {
		t.Fatalf("the panel says a value of %d letters goes past what it quotes, when the whole "+
			"body fits one line: %q", len([]rune(value)), line)
	}
	// The sign of a double drawing: the entity for an entity's own first letters. Read back
	// through one unescaping, the line is not the record any more.
	for _, drawn := range []string{"&amp;amp;", "&amp;lt;", "&amp;gt;", "&amp;#", "&amp;quot;"} {
		if strings.Contains(line, drawn) {
			t.Errorf("the panel drew %s inside the quotation, so the page escaped what was already "+
				"escaped: %q", drawn, line)
		}
	}
	if got, want := strings.Count(line, "&amp;"), strings.Count(value, "&"); got != want {
		t.Errorf("the quotation holds %d drawn ampersands for the %d the record wrote: %q",
			got, want, line)
	}
	if held := html.UnescapeString(line); held != value {
		t.Errorf("the panel quoted %q of a version that held %q: the reader is handed letters the "+
			"record does not hold, or loses letters it does", held, value)
	}
}
