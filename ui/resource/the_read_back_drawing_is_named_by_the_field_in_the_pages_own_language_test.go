package resource_test

// A drawing has no words of its own: what the screen calls the swatch in a row and
// the wide drawing on a record is the field's label, which is the accessible name a
// person hears when the line is nothing they can read as text. The field's label is
// the author's English until the request's copy seam answers the key ui/resource
// derives for it (`hints.<module>/<entity>.field.<name>.label`), so
// a reading that took the label from anywhere but that seam would name the drawing in
// the control's language on a page answering in another — the same half-translated page
// a dropped sentence leaves, and invisible to a markup assertion that only ever renders
// English fields.
//
// This renders one declared, translated drawing field as both reading places on a
// pt-PT page and asks each for the swatch named by the catalogue's own word, and for
// none of the declared English the seam replaces.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTheReadBackDrawingIsNamedByTheFieldInThePagesOwnLanguage(t *testing.T) {
	r := resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
		Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
		Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "drawing", Type: entity.TypeSketch, Presentation: entity.FieldHints{Label: "Drawing"}}}}}
	show := resource.Options{Workspace: "/app", Home: "Dashboard",
		Locale: speak("pt-PT", spoken{words: map[string]string{"hints.drawing/drawing.field.drawing.label": "Desenho"}})}
	row := map[string]any{"id": "1", "drawing": entity.Sketch{{{10, 20, 0}, {300, 400, 90}}}}
	for _, place := range []struct {
		name string
		body func() string
	}{
		{"the row's swatch", func() string {
			return render(t, resource.List(r, show, []map[string]any{row}, 1, 1, "", false).Body)
		}},
		{"the record's reading", func() string {
			return render(t, resource.Detail(r, show, row, false).Body)
		}},
	} {
		body := place.body()
		if !strings.Contains(body, `points="10,20 300,400"`) {
			t.Fatalf("%s draws none of the saved strokes, so nothing here asks who it is named by: %s", place.name, body)
		}
		if !strings.Contains(body, `aria-label="Desenho"`) {
			t.Errorf("%s of a drawing on a pt-PT page carries no accessible name %q, the words the request's seam keeps for hints.drawing/drawing.field.drawing.label",
				place.name, "Desenho")
		}
		if strings.Contains(body, `aria-label="Drawing"`) {
			t.Errorf("%s of a drawing on a pt-PT page is named %q, the declared English the seam answers over: one page then reads its own field in two languages",
				place.name, "Drawing")
		}
	}
}
