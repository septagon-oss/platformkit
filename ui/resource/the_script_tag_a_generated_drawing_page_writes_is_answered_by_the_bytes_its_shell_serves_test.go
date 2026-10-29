package resource_test

// A drawing takes a line on the page whose finished markup asked for the
// controller and whose server answers that ask. The two cases beside this one
// stop at the name a page carries: one compares it with a literal of its own,
// one opens it in the tree ui.Assets builds. Neither asks the question the
// browser asks — whether the address the document wrote is answered. So this
// renders the edit form as a whole document of one shell, reads the module URLs
// out of the emitted markup, and asks each of a file server mounted over that
// same tree at the prefix the chrome names, the way the shells mount it.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// drawingAssets is the prefix one shell serves ui.Assets under, as its chrome
// names it and as modules/admin mounts it.
const drawingAssets = "/admin/assets"

// moduleTagSrc reads the address of every module script out of a finished page.
var moduleTagSrc = regexp.MustCompile(`src="([^"]+)" type="module"`)

// TestTheScriptTagAGeneratedDrawingPageWritesIsAnsweredByTheBytesItsShellServes
// asks the finished page, not the view: it renders the edit form of a resource
// whose one field edits a drawing into a document of one shell, takes every
// module address the shell wrote into that markup, and fetches each from a file
// server over ui.Assets mounted at the prefix the chrome carries. A page that
// writes no address for its board, or one the mount does not answer, fails.
func TestTheScriptTagAGeneratedDrawingPageWritesIsAnsweredByTheBytesItsShellServes(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "drawing": entity.Sketch{{{10, 20, 0}, {300, 400, 90}}}}
	r := resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
		Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
		Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "drawing", Type: entity.TypeSketch}}}}
	sheet := ui.Compose(design.Default())
	view := resource.Form(r, opts, r.Screen+"/1/edit", "Drawing", row, nil, "", false)
	page := render(t, []g.Node{document.Document(
		document.Chrome{Brand: "Kit", Assets: drawingAssets, Stylesheet: sheet},
		document.Request{Path: r.Screen + "/1/edit"}, view, h.Main())})

	wanted := moduleTagSrc.FindAllStringSubmatch(page, -1)
	if len(wanted) == 0 {
		t.Fatalf("the drawing form wrote no module script at all, so its board has no controller to load:\n%s", page)
	}
	if !strings.Contains(page, `src="`+drawingAssets+"/js/sketch.js"+`" type="module"`) {
		t.Fatalf("the drawing form names %v as modules but writes no address for the board it draws: %v", view.Modules, wanted)
	}

	quiet := render(t, []g.Node{document.Document(
		document.Chrome{Brand: "Kit", Assets: drawingAssets, Stylesheet: sheet},
		document.Request{Path: note().Screen}, resource.Form(note(), opts, note().Screen+"/new", "Note", nil, nil, "", true), h.Main())})
	if strings.Contains(quiet, `type="module"`) {
		t.Errorf("a form that draws no board writes a module tag anyway:\n%s", quiet)
	}

	mount := http.NewServeMux()
	mount.Handle(drawingAssets+"/", http.StripPrefix(drawingAssets+"/", http.FileServerFS(ui.Assets(sheet))))
	for _, match := range wanted {
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, match[1], nil))
		if rec.Code != http.StatusOK {
			t.Errorf("the page tells the browser to load %q, which its own shell answers %d, so every board on that page renders as a canvas nobody can draw on", match[1], rec.Code)
			continue
		}
		if rec.Body.Len() == 0 {
			t.Errorf("the page tells the browser to load %q and its shell answers it with no bytes", match[1])
		}
	}
}
