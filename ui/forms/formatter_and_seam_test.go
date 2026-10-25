package forms_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// TestTheFormatterAnswersThePhraseWhenBothSeamsAreComposed: a render that composes
// the request's formatter and still carries the older seam is the shape
// ui/resource composes for every generated screen, so which of the two answers is
// what a person reads. The formatter must answer: it is the only seam that
// interpolates, and the older one is asked with a catalogue key it cannot put a
// value into. Answered the other way round, a translated catalogue that holds the
// key returns its verbs to the browser — fmt says so with "%!s(BADINDEX)" — and the
// number that measures wrapped copy still rises, because a mangled name carries the
// pseudo-locale's mark like any other answered phrase.
func TestTheFormatterAnswersThePhraseWhenBothSeamsAreComposed(t *testing.T) {
	for _, want := range []struct{ preference, name string }{
		{"en", "Bold (Ctrl+B)"},
		{"pt-PT", "Ctrl+B — Negrito"},
	} {
		selected := locale.SelectLocale(
			xtext.Load("en", xtext.Source{FS: forms.Catalogues(), Name: "ui/forms"}), want.preference)
		props := forms.ControlProps{
			Field:  richtextField(),
			ID:     "f",
			Format: selected,
			// The seam ui/resource still composes beside Format: it answers a key
			// from the catalogue and carries no argument at all.
			Text: func(key, fallback string) string { return selected.Text(key, fallback) },
		}
		out := drawn(t, forms.Control(props))
		if !strings.Contains(out, `aria-label="`+want.name+`"`) {
			t.Errorf("%s composes both seams and announces the bold command with something else:\n%s",
				want.preference, out)
		}
		if strings.Contains(out, "%!") || strings.Contains(out, "%[") {
			t.Errorf("%s prints a formatting verb where a person reads a command name:\n%s",
				want.preference, out)
		}
	}
}

// TestAnImmutableFieldAddsItsNoteToTheFieldsOwnLine: the note that says who may
// change a value belongs to the control and arrives for every widget, not only the
// two that draw a note of their own. The field's own documentation is the caller's
// sentence and stands as it was given.
func TestAnImmutableFieldAddsItsNoteToTheFieldsOwnLine(t *testing.T) {
	field := forms.Field{Definition: entity.Field{
		Name: "state", Type: entity.TypeString, Enum: []string{"open", "done"},
		Doc: "Where the task stands.",
	}, Label: "State"}
	out := spokenForm(t, "pt-PT", forms.ControlProps{Field: field, ID: "f", Immutable: true})
	paragraph := between(t, out, `id="f-help"`, "</p>")
	for _, want := range []string{"<span>Where the task stands.</span>",
		"<span>Alterado por um comando próprio, não por este formulário.</span>"} {
		if !strings.Contains(paragraph, want) {
			t.Errorf("the help paragraph of a read-only control does not draw %q as its own sentence: %s",
				want, paragraph)
		}
	}
	if !strings.Contains(out, `aria-describedby="f-help"`) {
		t.Errorf("the read-only control does not name the paragraph that says why it is read-only:\n%s", out)
	}
	mutable := drawn(t, forms.Control(forms.ControlProps{Field: field, ID: "f"}))
	if strings.Contains(mutable, "comando próprio") || strings.Contains(mutable, "not by this form") {
		t.Errorf("a control a person may change said it was changed by a command of its own:\n%s", mutable)
	}
}
