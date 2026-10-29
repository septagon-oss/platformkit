package resource_test

// A command that asks for a drawing is answered on the page that offers it: the
// dialog on the list and the one below a record carry the same board the edit form
// does, and that board only takes a line once the page loads the drawing script.
// A page names its own scripts in document.View.Modules — an editable drawing field
// gets the script from the field itself, and a list or a record gets it only from
// the commands its caller may use. With that answer made empty, every board in a
// command dialog still renders, inert: a canvas a person presses and cannot draw on,
// the form free to post the empty drawing it started with.
//
// So this asks each page that draws such a board to name the script its own markup
// binds, and asks the same two pages of commands that ask for no drawing to name
// none: an add-alike would answer the first half and ship an editor to every record.

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// sketchBoardScript is the drawing board's controller as the shell serves it.
const sketchBoardScript = "js/sketch.js"

// drawings is the drawing resource offered both command shapes: one asked of the
// collection, whose dialog the list draws, and one of a row, whose dialog the
// record draws, each taking argument as its one argument.
func drawings(argument entity.Field) resource.Resource {
	return resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
		Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
		Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "drawing", Type: entity.TypeSketch}}},
		Commands: []resource.Command{
			{Verb: "batch", Label: "Trace over every row", Base: "/app/drawing/drawings", Collection: true, Fields: []entity.Field{argument}},
			{Verb: "trace", Label: "Trace over", Base: "/app/drawing/drawings/1", Fields: []entity.Field{argument}},
		}}
}

// TestACommandThatAsksForADrawingNamesTheScriptItsPageLoads renders the list and the
// record of a resource whose commands take a drawing and asks each for the drawing
// script among the modules it names, beside the board that script mounts; then asks
// the same two pages of commands that take no drawing for no script.
func TestACommandThatAsksForADrawingNamesTheScriptItsPageLoads(t *testing.T) {
	row := map[string]any{"id": "1", "drawing": entity.Sketch{{{10, 20, 0}, {300, 400, 90}}}}
	for _, asked := range []entity.Field{{Name: "over", Type: entity.TypeSketch}, {Name: "note", Type: entity.TypeString}} {
		drawing := asked.Type == entity.TypeSketch
		r := drawings(asked)
		for _, page := range []struct {
			name string
			view func() document.View
		}{
			{"the list", func() document.View { return resource.List(r, opts, []map[string]any{row}, 1, 1, "", true) }},
			{"the record", func() document.View { return resource.Detail(r, opts, row, true) }},
		} {
			view := page.view()
			named := slices.Contains(view.Modules, sketchBoardScript)
			body := render(t, view.Body)
			if drawing && !strings.Contains(body, `data-sketch=""`) {
				t.Fatalf("%s draws no board for the command that asks for a drawing, so the script asked for below mounts on nothing here: %s", page.name, body)
			}
			if named && !drawing {
				t.Errorf("%s of a resource no command asks a drawing for names the drawing script %q anyway, an editor no board on this page binds: %v",
					page.name, sketchBoardScript, view.Modules)
			}
			if drawing && !named {
				t.Errorf("%s of a resource whose command asks for a drawing names none of the modules %q, so its board is a canvas nobody can draw on and the dialog posts the empty drawing it started with: %v",
					page.name, sketchBoardScript, view.Modules)
			}
		}
	}
}
