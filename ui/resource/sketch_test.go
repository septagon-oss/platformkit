package resource_test

import (
	"html"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestSketchResourcePreservesReadingAndRefusedDraft(t *testing.T) {
	r := resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
		Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
		Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true}, {Name: "title", Type: entity.TypeString}, {Name: "drawing", Type: entity.TypeSketch}},
	}}
	row := map[string]any{"id": "1", "title": "Kept", "drawing": entity.Sketch{{{1, 2, 0}, {900, 800, 9007199254740991}}}}
	// A row reads the drawing at a swatch and the record at the width a person can
	// make a line out of; the geometry they are drawn from is the same SVG.
	for _, want := range []struct{ body, size string }{
		{render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body), `data-sketch-size="thumbnail"`},
		{render(t, resource.Detail(r, opts, row, false).Body), `data-sketch-size="full"`},
	} {
		if !strings.Contains(want.body, `points="1,2 900,800"`) || !strings.Contains(want.body, `aria-label="Drawing"`) || !strings.Contains(want.body, want.size) {
			t.Fatal(want.body)
		}
	}
	for _, raw := range []string{`[[[1,2,0],[900,800,9007199254740991]]]`, `[[[1.0,2,0]]]`, ""} {
		row["drawing"] = raw
		view := resource.Form(r, opts, "/save", "Edit drawing", row, map[string]string{"title": "Add a title."}, "", false)
		body := render(t, view.Body)
		if !strings.Contains(body, `name="drawing" value="`+html.EscapeString(raw)+`"`) || !slices.Contains(view.Modules, "js/sketch.js") {
			t.Fatal(body, view.Modules)
		}
	}
}
