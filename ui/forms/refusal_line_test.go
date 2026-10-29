package forms_test

// A refused write is read by the person who has to correct it, in the language they
// asked in: the heading the refusal is announced with, the line naming each field to
// correct, and the landmark that lists them are asked of the render's formatter, so
// the copy decides in what order the field and the complaint meet.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/forms"
)

// refused draws one richtext field the server refused, through the copy this package
// ships: the field's own widget is what makes the form draw the list of fields to
// correct, and the sentence naming it is the one whose word order is at stake.
func refused(t *testing.T, preference string) string {
	t.Helper()
	page, err := forms.Example("form", forms.Model{
		Fields: []forms.Field{richtextField()},
		Detail: "The text holds a style this workspace does not carry.",
		Errors: map[string]string{"body": "Write the text again without it"},
	}, forms.Options{Namespace: "f", Action: "/app/content/contents", Title: "Edit",
		Format: catalogueFor(preference)})
	if err != nil {
		t.Fatal(err)
	}
	return drawn(t, page.Node)
}

// TestARefusedFieldIsNamedInTheOrderTheLanguageWrote: the line naming a field to
// correct names two things — the field and what is wrong with it — and their order
// belongs to the copy. A reader who asked in Portuguese reads the complaint first and
// the field second, because that is how the catalogue for that language says it, and
// the same page still announces the refusal and lists the fields in that language.
func TestARefusedFieldIsNamedInTheOrderTheLanguageWrote(t *testing.T) {
	english := refused(t, "en")
	for _, want := range []string{"Body: Write the text again without it", "Fields to correct", "That could not be saved"} {
		if !strings.Contains(english, want) {
			t.Errorf("the refusal drawn in English no longer says %q:\n%s", want, english)
		}
	}
	portuguese := refused(t, "pt-PT")
	for _, want := range []string{"Write the text again without it — verifique Body", "Campos a corrigir", "Não foi possível guardar"} {
		if !strings.Contains(portuguese, want) {
			t.Errorf("the refusal drawn in Portuguese no longer says %q:\n%s", want, portuguese)
		}
	}
	if strings.Contains(portuguese, "Body: Write the text again without it") {
		t.Errorf("the refusal named the field in Go's order where the catalogue had written its own:\n%s", portuguese)
	}
	for _, page := range []string{english, portuguese} {
		if strings.Contains(page, "%!") || strings.Contains(page, "%[") {
			t.Errorf("a formatting verb reached a refused form:\n%s", page)
		}
	}
}
