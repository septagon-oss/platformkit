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
	for _, body := range []string{render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body), render(t, resource.Detail(r, opts, row, false).Body)} {
		if !strings.Contains(body, `points="1,2 900,800"`) || !strings.Contains(body, `aria-label="Drawing"`) {
			t.Fatal(body)
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
