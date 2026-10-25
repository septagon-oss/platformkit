package forms_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

// spokenForm draws one control through a copy table, which is the only way to ask
// where a language put a value inside a phrase: the shipped catalogues are the
// copy that ships, not a table this file could invent.
func spokenForm(t *testing.T, preference string, props forms.ControlProps) string {
	t.Helper()
	selected := locale.SelectLocale(xtext.Load("en", xtext.Source{FS: forms.Catalogues(), Name: "ui/forms"}), preference)
	props.Format = selected
	var out strings.Builder
	if err := forms.Control(props).Render(&out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func drawn(t *testing.T, node g.Node) string {
	t.Helper()
	var out strings.Builder
	if err := node.Render(&out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// richtextField is the one control whose accessible names carry a keystroke.
func richtextField() forms.Field {
	return forms.Field{Definition: entity.Field{
		Name: "body", Type: entity.TypeText, Widget: "richtext", Doc: "The published text.",
	}, Label: "Body"}
}

// TestAToolbarNameCarriesItsKeystrokeWhereTheLanguagePutsIt: the bold button is
// announced by one sentence, and the sentence is the catalogue's. English closes
// the keystroke in a parenthesis after the word; the shipped Portuguese copy opens
// with it. Both are one message, so neither is assembled in Go.
func TestAToolbarNameCarriesItsKeystrokeWhereTheLanguagePutsIt(t *testing.T) {
	for _, want := range []struct{ preference, name string }{
		{"en", "Bold (Ctrl+B)"},
		{"pt-PT", "Ctrl+B — Negrito"},
	} {
		out := spokenForm(t, want.preference, forms.ControlProps{Field: richtextField(), ID: "f"})
		if !strings.Contains(out, `aria-label="`+want.name+`"`) {
			t.Errorf("%s announces the bold command as something else:\n%s", want.preference, out)
		}
		if word := map[string]string{"en": "Bold", "pt-PT": "Negrito"}[want.preference]; !strings.Contains(out, ">"+word+"</span>") && !strings.Contains(out, ">"+word+"<") {
			t.Errorf("%s shows the command by a visible word nobody can read: %s", want.preference, out)
		}
	}
	// A command with no keystroke is announced by its title and asks no name: the
	// pair of table fields is what keeps that true, and the render says so.
	out := spokenForm(t, "pt-PT", forms.ControlProps{Field: richtextField(), ID: "f"})
	if strings.Contains(out, `aria-label="Strikethrough`) || !strings.Contains(out, `aria-label="Rasurado"`) {
		t.Errorf("a command with no keystroke should be announced by its own title:\n%s", out)
	}
}

// TestASelectPlacesItsLabelInsideTheSentenceTheLanguageWrote: the unchosen state
// of a select names the field, and the field's label is a value the copy places.
// English says "Choose a Status"; the shipped Portuguese copy puts the label first
// and asks for an option after it. No Go casing rule reaches the label on the way.
func TestASelectPlacesItsLabelInsideTheSentenceTheLanguageWrote(t *testing.T) {
	field := forms.Field{Definition: entity.Field{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}}, Label: "Status"}
	for _, want := range []struct{ preference, placeholder string }{
		{"en", "Choose a Status"},
		{"pt-PT", "Status: selecione uma opção"},
	} {
		out := spokenForm(t, want.preference, forms.ControlProps{Field: field, ID: "f"})
		if !strings.Contains(out, `>`+want.placeholder+`</option>`) {
			t.Errorf("%s leaves the select's unchosen state saying something else:\n%s", want.preference, out)
		}
	}
}

// TestAFieldDrawsEachHelpSentenceOnItsOwn: the line under a control can come from
// two authors — the field's own documentation, and the note its form writes about
// the control it chose. Each is drawn as its own sentence inside the one paragraph
// aria-describedby names, so neither is a fragment of the other's sentence.
func TestAFieldDrawsEachHelpSentenceOnItsOwn(t *testing.T) {
	list := forms.Field{Definition: entity.Field{
		Name: "tags", Type: entity.TypeList, Widget: "text", Doc: "Labels for the record.",
	}, Label: "Tags"}
	out := spokenForm(t, "en", forms.ControlProps{Field: list, ID: "f"})
	if !strings.Contains(out, `aria-describedby="f-help"`) {
		t.Errorf("the list control does not name its help paragraph:\n%s", out)
	}
	paragraph := between(t, out, `id="f-help"`, "</p>")
	for _, want := range []string{"<span>Labels for the record.</span>", "<span>Comma separated.</span>"} {
		if !strings.Contains(paragraph, want) {
			t.Errorf("the help paragraph does not draw %q as its own sentence: %s", want, paragraph)
		}
	}

	// A field with nothing to say about its control keeps the plain sentence it
	// always drew, with no element around it.
	plain := forms.Control(forms.ControlProps{Field: forms.Field{
		Definition: entity.Field{Name: "title", Type: entity.TypeString, Doc: "A useful name."}, Label: "Title",
	}, ID: "f"})
	if got := between(t, drawn(t, plain), `id="f-help"`, "</p>"); strings.Contains(got, "<span>") {
		t.Errorf("a field with one help sentence wrapped it anyway: %s", got)
	}
}

// TestAControlWithoutAFormatterDrawsItsPhrasesAsWritten: the source text is a
// finished sentence, formatted with the values its own verbs ask for, and the
// older seam that takes no argument is never handed a verb.
func TestAControlWithoutAFormatterDrawsItsPhrasesAsWritten(t *testing.T) {
	only := drawn(t, forms.Control(forms.ControlProps{Field: richtextField(), ID: "f"}))
	seam := drawn(t, forms.Control(forms.ControlProps{Field: richtextField(), ID: "f",
		Text: func(_, fallback string) string { return fallback }}))
	for name, out := range map[string]string{"no formatter": only, "the older seam": seam} {
		if !strings.Contains(out, `aria-label="Bold (Ctrl+B)"`) {
			t.Errorf("%s draws the bold command's name as something else:\n%s", name, out)
		}
		if strings.Contains(out, "%[") || strings.Contains(out, "%!(") {
			t.Errorf("%s prints a formatting verb where a person reads a name:\n%s", name, out)
		}
	}
}

// between cuts the markup one element holds, from just after the anchor to the
// closing tag, which is where the shape of a sentence list is visible.
func between(t *testing.T, source, anchor, close string) string {
	t.Helper()
	start := strings.Index(source, anchor)
	if start < 0 {
		t.Fatalf("nothing in the render holds %q:\n%s", anchor, source)
	}
	open := strings.Index(source[start:], ">")
	end := strings.Index(source[start:], close)
	if open < 0 || end < 0 {
		t.Fatalf("%q has no element to read:\n%s", anchor, source)
	}
	return source[start+open+1 : start+end]
}
