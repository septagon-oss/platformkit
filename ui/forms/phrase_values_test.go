package forms_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// catalogueFor reads the copy this package ships, which is the only copy a caller
// can be asked to answer with.
func catalogueFor(preference string) locale.Locale {
	return locale.SelectLocale(
		xtext.Load("en", xtext.Source{FS: forms.Catalogues(), Name: "ui/forms"}), preference)
}

// TestAPhraseThatTakesAValueIsNeverAskedOfTheSeamThatCarriesNone: the older seam
// answers a key with no value, so the copy it returns for a phrase that asks for
// one arrives with that value's verb unfulfilled — a catalogue that holds the key is
// enough for that, and a screen reader is left announcing "Negrito (%!s(BADINDEX))"
// for the bold command. A render that composes only that seam is what every caller
// written before the formatter looks like, so a phrase with a value in it is answered
// from its source text instead, which is a finished sentence.
func TestAPhraseThatTakesAValueIsNeverAskedOfTheSeamThatCarriesNone(t *testing.T) {
	selected := catalogueFor("pt-PT")
	out := drawn(t, forms.Control(forms.ControlProps{
		Field: richtextField(), ID: "f",
		Text: func(key, _ string) string { return selected.Text(key, "") },
	}))
	if !strings.Contains(out, `aria-label="Bold (Ctrl+B)"`) {
		t.Errorf("a render that composes only the older seam did not draw the command's source sentence:\n%s", out)
	}
	if strings.Contains(out, "%!") {
		t.Errorf("a formatting verb reached the page through the seam that carries no value:\n%s", out)
	}
	// A phrase with no value in it is still asked of that seam, which is all it
	// was ever able to answer, and the Portuguese copy answers it.
	if !strings.Contains(out, "Formatação") {
		t.Errorf("the seam that answers keys stopped answering the phrases it can carry:\n%s", out)
	}
}
