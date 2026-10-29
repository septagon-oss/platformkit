package entity_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// Four word-lists decide whether a resource's author may mount: the icon a card wears,
// the tone a value is read in, the format a field prints with, the visibility it is
// hidden at. They are exported because the gate that refuses another word lives in
// kit/rest and the drawing of each word lives in ui/, and exported slices are writable by
// whoever holds them — which is everybody in the program. The accessors are the same
// words with a copy between the list and the caller; the lists themselves stay exported
// only for the frozen surface that already reads them.
func TestTheReadingVocabulariesComeOutAsCopies(t *testing.T) {
	lists := map[string]struct {
		read   func() []string
		member string
	}{
		"icons":      {entity.IconNames, "person"},
		"tones":      {entity.ToneNames, "danger"},
		"formats":    {entity.FormatNames, "money"},
		"visibility": {entity.VisibilityNames, "hidden"},
	}
	for name, list := range lists {
		handed := list.read()
		if len(handed) == 0 {
			t.Fatalf("%s: the vocabulary is empty", name)
		}
		if !slices.Contains(handed, list.member) {
			t.Errorf("%s: %v does not admit %q", name, handed, list.member)
		}
		for i := range handed {
			handed[i] = "tampered"
		}
		standing := list.read()
		if slices.Contains(standing, "tampered") {
			t.Errorf("%s: writing the answer wrote the kernel's list: %v", name, standing)
		}
		if first, second := list.read(), list.read(); !slices.Equal(first, second) {
			t.Errorf("%s: two reads of the vocabulary disagree: %v then %v", name, first, second)
		}
	}
}
