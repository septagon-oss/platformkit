package resource_test

// The drawing board takes a line only on a page that loaded its controller, and a
// page names that controller as a bare path: ui/resource writes it into
// document.View.Modules from the editable field on a form and from the commands a
// list or a record offers, and the shell turns every name it is given into a
// <script src> without asking whether a script of that name exists. The asset
// under ui/assets, the two places that name it and the shell are three files that
// have to agree, and the failure when they stop agreeing is the one these cases
// exist to prevent: a board that renders and cannot be drawn on. So every module a
// page with a drawing names has to open in the tree a shell actually serves.

import (
	"io/fs"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestEveryScriptAGeneratedDrawingPageNamesIsAScriptTheShellServes renders the form,
// the list and the record of a resource that carries a drawing — editable on the
// form, and asked for by one command of the collection and one of a row — and asks
// of each page that every module it names opens in the asset tree the shell serves.
func TestEveryScriptAGeneratedDrawingPageNamesIsAScriptTheShellServes(t *testing.T) {
	row := map[string]any{"id": "1", "drawing": entity.Sketch{{{10, 20, 0}, {300, 400, 90}}}}
	asked := entity.Field{Name: "over", Type: entity.TypeSketch}
	r := resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
		Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
		Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "drawing", Type: entity.TypeSketch}}},
		Commands: []resource.Command{
			{Verb: "batch", Label: "Trace over every row", Base: "/app/drawing/drawings", Collection: true, Fields: []entity.Field{asked}},
			{Verb: "trace", Label: "Trace over", Base: "/app/drawing/drawings/1", Fields: []entity.Field{asked}},
		}}
	served := ui.Assets(ui.Compose(design.Default()))
	for _, page := range []struct {
		name string
		view func() document.View
	}{
		{"the form", func() document.View { return resource.Form(r, opts, r.Screen+"/1", "Drawing", row, nil, "", false) }},
		{"the list", func() document.View { return resource.List(r, opts, []map[string]any{row}, 1, 1, "", true) }},
		{"the record", func() document.View { return resource.Detail(r, opts, row, true) }},
	} {
		modules := page.view().Modules
		if len(modules) == 0 {
			t.Errorf("%s of a resource whose form edits a drawing and whose commands ask for one names no module at all, so the board it draws has nobody to draw with", page.name)
			continue
		}
		for _, name := range modules {
			if _, err := fs.Stat(served, name); err != nil {
				t.Errorf("%s names the module %q, which the tree the shell serves does not hold (%v), so every board on that page renders as a canvas nobody can draw on: %v",
					page.name, name, err, modules)
			}
		}
	}
}
