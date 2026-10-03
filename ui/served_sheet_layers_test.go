package ui_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// The boundary the other files read but nobody held: the
// composed sheet is not the artifact a browser loads. ui.Assets is what turns it into
// app.css, and its overlay answers its own files before the trees a consumer hands
// it, so a consumer's own app.css cannot replace the layered sheet. Were that lookup
// to flip, an unlayered file named app.css would be served — and an unlayered rule
// outranks every rule inside every layer the order statement names, which is the
// precedence this task exists to refuse, arriving through the asset tree rather than
// through ui.Extra and refuseClientSheet. Nothing else in the tree serves a consumer
// filesystem alongside the sheet it composed: ui_test.go's
// TestAssetsServeSheetsControllersAndOverlays adds a script and never a file named
// app.css, and gallery.css is only ever pinned against its own layer.
//
// Every assertion here runs on the bytes ui.Assets serves, not on what ui.Compose
// returns, so a sheet that is layered in Go and flattened on the way out fails here.

const r9Order = "@layer tokens, base, components, client;"

var r9Openings = []string{"@layer tokens {", "@layer base {", "@layer components {", "@layer client {"}

func servedLayeredSheet(t *testing.T, assets fs.FS, name string) string {
	t.Helper()
	body, err := fs.ReadFile(assets, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

// scanLayers separates what a sheet states from what it opens: one `@layer a, b;`
// statement, the blocks at depth 0 counted by balancing braces over quoted strings,
// and any rule or block the sheet leaves outside a layer.
func scanLayers(t *testing.T, sheet string) (statements, blocks, unlayered []string) {
	t.Helper()
	depth, inString := 0, byte(0)
	for _, line := range strings.Split(sheet, "\n") {
		trimmed := strings.TrimSpace(line)
		if depth == 0 && trimmed != "" {
			switch {
			case strings.HasSuffix(trimmed, ";"):
				if strings.HasPrefix(trimmed, "@layer") {
					statements = append(statements, trimmed)
				}
			case strings.HasPrefix(trimmed, "@layer"), strings.HasPrefix(trimmed, "@media"),
				strings.HasPrefix(trimmed, "@keyframes"):
				blocks = append(blocks, trimmed)
			case strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "}"):
				unlayered = append(unlayered, trimmed)
			}
		}
		for at := 0; at < len(line); at++ {
			c := line[at]
			switch {
			case inString != 0:
				if c == '\\' {
					at++
				} else if c == inString {
					inString = 0
				}
			case c == '\'' || c == '"':
				inString = c
			case c == '{':
				depth++
			case c == '}':
				depth--
			}
		}
	}
	return statements, blocks, unlayered
}

func TestTheServedSheetIsTheLayeredSheetInItsDeclaredOrder(t *testing.T) {
	app := ui.Compose(design.Default())
	sheet := servedLayeredSheet(t, ui.Assets(app), "app.css")
	if sheet != string(app.Body) {
		t.Fatal("the served app.css is not the composed sheet")
	}
	statements, blocks, unlayered := scanLayers(t, sheet)
	if strings.Join(statements, " | ") != r9Order {
		t.Fatalf("the served sheet states %v, want exactly the one order statement %q: the ranking has to be stated, and stated before every block that uses a name in it", statements, r9Order)
	}
	if strings.Join(blocks, " | ") != strings.Join(r9Openings, " | ") {
		t.Errorf("the served sheet opens %v at depth 0, want exactly %v in that order", blocks, r9Openings)
	}
	if len(unlayered) != 0 {
		t.Errorf("the served sheet carries %d rule or block outside every layer, first %d of them %v: an unlayered rule outranks every rule inside a layer", len(unlayered), min(5, len(unlayered)), unlayered[:min(5, len(unlayered))])
	}
}

func TestAConsumerCannotServeItsOwnAppCSSOverTheLayeredSheet(t *testing.T) {
	app := ui.Compose(design.Default())
	// The shape a client would write to leave its layer: a static file beside the
	// application, unlayered, naming a kernel role. The same declaration inside
	// ui.Extra meets refuseClientSheet; served as app.css it would never meet it.
	unlayered := ".pk-card { background: var(--pk-role-surface-default) }"
	assets := ui.Assets(app, fstest.MapFS{"app.css": {Data: []byte(unlayered)}})
	served := servedLayeredSheet(t, assets, "app.css")
	if served != string(app.Body) {
		t.Fatalf("a consumer's own app.css replaced the composed sheet: %d bytes served, %d composed", len(served), len(app.Body))
	}
	if !strings.HasPrefix(strings.TrimSpace(served), r9Order) {
		t.Fatalf("the served sheet no longer opens with the layer order statement; it opens with %.60q", served)
	}
}

func TestTheServedGallerySheetLandsInsideTheComponentsLayer(t *testing.T) {
	// gallery.css states no order of its own: it may rank only where app.css — which
	// every shell links first — says the components layer ranks. So it must open one
	// block and nothing else, and no rule outside it.
	statements, blocks, unlayered := scanLayers(t, servedLayeredSheet(t, ui.Assets(ui.Compose(design.Default())), "gallery.css"))
	if len(statements) != 0 {
		t.Errorf("gallery.css states its own layer order %v; the order belongs to app.css, which is linked first", statements)
	}
	if len(blocks) != 1 || blocks[0] != "@layer components {" {
		t.Errorf("gallery.css opens %v, want one @layer components block", blocks)
	}
	if len(unlayered) != 0 {
		t.Errorf("gallery.css carries %d rule or block outside its layer, first %d of them %v", len(unlayered), min(5, len(unlayered)), unlayered[:min(5, len(unlayered))])
	}
}
