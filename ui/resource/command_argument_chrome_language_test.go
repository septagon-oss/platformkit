package resource_test

// A command's arguments are asked for on a generated screen whose every other word
// is the request's: the button, the warning, each field's label and help line. The
// line under an argument is what the form says about the control it chose, so it
// belongs to the same language and is answered through the same formatter.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestACommandsArgumentFormDrawsItsNoteInTheRequestsLanguage: a list argument gets
// the same control the edit form gives a list field, including the sentence that
// says how to fill it. The copy table answers the key the control asks with; a form
// that composed no formatter would draw the sentence this package was written in
// beside a button drawn in the reader's own.
func TestACommandsArgumentFormDrawsItsNoteInTheRequestsLanguage(t *testing.T) {
	r := lines()
	r.Commands = []resource.Command{{
		Verb: "assign", Label: "Assign", Base: r.Screen,
		Fields: []entity.Field{{Name: "tags", Type: entity.TypeList, Elem: entity.TypeString,
			Doc: "Labels for the record."}},
	}}
	o := resource.Options{Locale: speak("pt-PT", spoken{words: map[string]string{
		"forms.field.note.list": "Separados por vírgulas.",
	}})}
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "A line"}
	body := render(t, resource.Detail(r, o, row, true).Body)
	if !strings.Contains(body, "Separados por vírgulas.") {
		t.Errorf("a command's argument line was not answered in the request's language:\n%s", body)
	}
	if strings.Contains(body, "Comma separated.") {
		t.Errorf("a command's argument form fell back to source text the screen's own copy answers:\n%s", body)
	}
}
