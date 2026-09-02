package ui_test

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui"
)

func TestStylesheetCarriesTokensRolesBaseAndUtilities(t *testing.T) {
	t.Parallel()
	css := string(ui.Stylesheet())
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
	if string(ui.Stylesheet()) != string(ui.Stylesheet()) {
		t.Fatal("two calls produced different stylesheets")
	}
	if len(ui.Fingerprint()) != 16 {
		t.Fatalf("the fingerprint is %q", ui.Fingerprint())
	}
}

func TestAssetsServeTheStylesheetAndEveryController(t *testing.T) {
	t.Parallel()
	assets := ui.Assets()
	f, err := assets.Open("app.css")
	if err != nil {
		t.Fatalf("app.css: %v", err)
	}
	body, err := io.ReadAll(f)
	if err != nil || len(body) != len(ui.Stylesheet()) {
		t.Fatalf("app.css read %d bytes (err %v), want %d", len(body), err, len(ui.Stylesheet()))
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
