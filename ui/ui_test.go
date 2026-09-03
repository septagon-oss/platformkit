package ui_test

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

func TestStylesheetCarriesTokensRolesBaseAndUtilities(t *testing.T) {
	t.Parallel()
	css := string(ui.Stylesheet(design.Default()))
	for _, want := range []string{
		"--pk-color-surface-primary:", // the theme's tokens
		"--pk-role-surface-brand:",    // the roles, in terms of them
		"box-sizing: border-box",      // the base layer
		".inline-flex {",              // a utility a component declared
		"@media (prefers-color-scheme: dark)",
		"@media (prefers-reduced-motion: reduce)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet has no %q", want)
		}
	}
	if strings.Count(css, "{") != strings.Count(css, "}") {
		t.Error("the stylesheet has unbalanced braces")
	}
}

func TestStylesheetIsStableAndFingerprinted(t *testing.T) {
	t.Parallel()
	if string(ui.Stylesheet(design.Default())) != string(ui.Stylesheet(design.Default())) {
		t.Fatal("two calls produced different stylesheets")
	}
	if len(ui.Fingerprint(design.Default())) != 16 {
		t.Fatalf("the fingerprint is %q", ui.Fingerprint(design.Default()))
	}
}

func TestAssetsServeTheStylesheetAndEveryController(t *testing.T) {
	t.Parallel()
	assets := ui.Assets(design.Default())
	f, err := assets.Open("app.css")
	if err != nil {
		t.Fatalf("app.css: %v", err)
	}
	body, err := io.ReadAll(f)
	if err != nil || len(body) != len(ui.Stylesheet(design.Default())) {
		t.Fatalf("app.css read %d bytes (err %v), want %d", len(body), err, len(ui.Stylesheet(design.Default())))
	}
	if info, err := fs.Stat(assets, "app.css"); err != nil || info.Size() != int64(len(body)) {
		t.Fatalf("app.css does not describe itself: %v %v", info, err)
	}
	for _, name := range ui.Controllers {
		if _, err := fs.ReadFile(assets, "js/"+name); err != nil {
			t.Errorf("controller %s: %v", name, err)
		}
	}
}

// TestThereAreFewControllers is the budget, spelled where it is decided rather
// than in a document nobody rereads. Eight is the ceiling the stage set; five
// is what the shell needs, and one of those is htmx.
func TestThereAreFewControllers(t *testing.T) {
	t.Parallel()
	if len(ui.Controllers) > 8 {
		t.Fatalf("there are %d browser controllers; the budget is 8", len(ui.Controllers))
	}
	ours := 0
	for _, name := range ui.Controllers {
		if !strings.HasSuffix(name, ".min.js") {
			ours++
		}
	}
	if ours > 6 {
		t.Fatalf("%d of the controllers are ours; the shell was meant to need a handful", ours)
	}
}

// TestAClientsOwnPaletteIsTheOnlyThingThatChanges is the theme seam: a client
// supplies two Themes and gets a stylesheet of its own, with the same rules,
// the same class names and a fingerprint that says the bytes moved. Nothing
// above the tokens knows.
func TestAClientsOwnPaletteIsTheOnlyThingThatChanges(t *testing.T) {
	t.Parallel()
	mine := design.Default()
	mine.Light.AccentDefault = "#b3002d"
	mine.Dark.AccentDefault = "#ff5c7a"

	stock, client := string(ui.Stylesheet(design.Default())), string(ui.Stylesheet(mine))
	if stock == client {
		t.Fatal("a client's own accent produced the same stylesheet")
	}
	if !strings.Contains(client, "--pk-color-accent-default: #b3002d") {
		t.Error("the client's accent is not in the token layer")
	}
	if strings.Contains(client, "--pk-color-accent-default: #0f5d4e") {
		t.Error("the shipped accent survived into the client's stylesheet")
	}
	if ui.Fingerprint(mine) == ui.Fingerprint(design.Default()) {
		t.Error("two palettes share a fingerprint, so a browser would keep the wrong one")
	}
	// Only the tokens moved: every line that is not a --pk-color-* declaration
	// is the same text, which is what makes this a seam rather than a second
	// design system.
	withoutTokens := func(sheet string) string {
		var kept []string
		for _, line := range strings.Split(sheet, "\n") {
			if !strings.Contains(line, "--pk-color-") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}
	if withoutTokens(stock) != withoutTokens(client) {
		t.Error("a palette changed a rule, not just a token")
	}
	// And it is composed once per palette.
	if &ui.Stylesheet(mine)[0] != &ui.Stylesheet(mine)[0] {
		t.Error("the stylesheet is recomposed on every call")
	}
}

// TestTheGallerySheetIsTheDifference: the second sheet carries the rules the
// first does not, so loading both is not loading `.flex` twice.
func TestTheGallerySheetIsTheDifference(t *testing.T) {
	t.Parallel()
	app, gallery := string(ui.Stylesheet(design.Default())), string(ui.GalleryStylesheet())
	if len(gallery) == 0 || len(gallery) > len(app) {
		t.Fatalf("gallery.css is %d bytes against app.css's %d", len(gallery), len(app))
	}
	if strings.Contains(gallery, "--pk-color-") || strings.Contains(gallery, "box-sizing") {
		t.Error("the second sheet carries tokens or the base layer, which the first already did")
	}
	for _, line := range strings.Split(gallery, "\n") {
		selector, _, isRule := strings.Cut(strings.TrimSpace(line), " {")
		if !isRule || !strings.HasPrefix(selector, ".") {
			continue
		}
		if strings.Contains(app, "\n"+selector+" {") {
			t.Errorf("%s is in both sheets", selector)
		}
	}
}
