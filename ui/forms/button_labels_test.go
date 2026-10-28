package forms_test

// The two buttons a form adds when nobody named them, and the heading a refused
// write is announced with, are chrome like every other sentence here: they are
// asked of the render's formatter, so a caller that named its own word keeps it and
// a reader who asked in another language is not left with English.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// TestTheFormDrawsItsOwnButtonsInTheLanguageTheRenderAnswers: the two buttons a
// form adds when nobody named them are chrome like every other phrase here, so a
// render that composes a formatter reads "Cancelar" and "Guardar" and a render that
// composes none keeps the words this package was written in. A caller that named a
// button keeps the word it named.
func TestTheFormDrawsItsOwnButtonsInTheLanguageTheRenderAnswers(t *testing.T) {
	model := forms.Model{Fields: []forms.Field{{Definition: entity.Field{
		Name: "title", Type: entity.TypeString, Doc: "What the record is called.",
	}, Label: "Title"}}}
	own := func(preference, cancel, submit string) string {
		t.Helper()
		page, err := forms.Example("form", model, forms.Options{
			Namespace: "f", Action: "/app/task/tasks", Title: "New",
			Format: catalogueFor(preference), CancelLabel: cancel, SubmitLabel: submit,
		})
		if err != nil {
			t.Fatal(err)
		}
		return drawn(t, page.Node)
	}
	for _, want := range []struct {
		preference string
		buttons    []string
	}{
		{"en", []string{"Cancel", "Save"}},
		{"pt-PT", []string{"Cancelar", "Guardar"}},
	} {
		out := own(want.preference, "", "")
		for _, label := range want.buttons {
			if !strings.Contains(out, ">"+label+"<") {
				t.Errorf("%s draws one of the form's own buttons as something else: %s missing from\n%s",
					want.preference, label, out)
			}
		}
	}
	out := own("pt-PT", "Back", "Publish")
	for _, label := range []string{"Back", "Publish"} {
		if !strings.Contains(out, ">"+label+"<") {
			t.Errorf("the button a caller named lost its word: %s missing from\n%s", label, out)
		}
	}
	if strings.Contains(out, ">Cancelar<") || strings.Contains(out, ">Guardar<") {
		t.Errorf("a button a caller named was answered by the catalogue instead:\n%s", out)
	}
}
