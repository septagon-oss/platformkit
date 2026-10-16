package transport_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// Event names remain literal broker tokens: neither control characters nor
// Unicode whitespace can introduce a second spelling of an accepted name.
func TestEventNamesRefuseControlCharactersAndUnicodeWhitespace(t *testing.T) {
	for _, separator := range []string{"\x00", "\t", "\n", "\r", "\f", "\u00a0", "\u200b", "\u2028"} {
		for _, name := range []string{separator + "notes.created", "notes." + separator + "created", "notes.created" + separator} {
			if transport.ValidName(name) {
				t.Errorf("ValidName(%q) accepted a nonliteral broker token", name)
			}
		}
	}
	for _, name := range []string{"notes.created", "notes.note.created", "notes_v2.note_2.created"} {
		if !transport.ValidName(name) {
			t.Errorf("ValidName(%q) refused a literal event name", name)
		}
	}
}
