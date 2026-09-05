package ui_test

import (
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestComposeCarriesTokensRolesBaseAndUtilities(t *testing.T) {
	t.Parallel()
	sheet := string(ui.Compose(design.Default()).Body)
	for _, want := range []string{
		"--pk-color-surface-primary:", // the theme's tokens
		"--pk-role-surface-brand:",    // the roles, in terms of them
		"box-sizing: border-box",      // the base layer
		".inline-flex {",              // a utility a component declared
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the stylesheet lacks %q", want)
		}
	}
}

func TestComposeIsDeterministicAndFingerprinted(t *testing.T) {
	t.Parallel()
	a, b := ui.Compose(design.Default()), ui.Compose(design.Default())
	if string(a.Body) != string(b.Body) || a.Fingerprint != b.Fingerprint {
		t.Fatal("two compositions of the same theme differ")
	}
	if len(a.Fingerprint) != 16 {
		t.Fatalf("the fingerprint is %q", a.Fingerprint)
	}
}

func TestComposeResolvesAConsumersListsAndRulesOnce(t *testing.T) {
	t.Parallel()
	// A list that repeats a class the components already declare (.flex) and
	// adds one they do not (.aspect-square); and a rule no class can express.
	flavour := style.New().Display(style.DisplayFlex).AspectSquare()
	grain := css.NewSheet()
	grain.Select(`[data-grain="pke-grain"] body::after`, css.Decl("content", css.Literal(`""`)))
	plain := ui.Compose(design.Default())
	extra := ui.Compose(design.Default(), ui.Extra{Lists: []style.ClassList{flavour}, Sheets: []*css.Sheet{grain}})
	body := string(extra.Body)
	if !strings.Contains(body, ".aspect-square {") {
		t.Fatal("the consumer's class has no rule")
	}
	if !strings.Contains(body, `[data-grain="pke-grain"] body::after`) {
		t.Fatal("the consumer's rule is missing")
	}
	if n := strings.Count(body, ".flex {"); n != 1 {
		t.Fatalf(".flex is emitted %d times; a shared utility has one rule", n)
	}
	if extra.Fingerprint == plain.Fingerprint {
		t.Fatal("a different sheet has the same fingerprint")
	}
}

func TestGalleryIsTheDifference(t *testing.T) {
	t.Parallel()
	app, gallery := string(ui.Compose(design.Default()).Body), string(ui.Gallery().Body)
	if !strings.Contains(gallery, ".animate-pulse {") {
		t.Fatal("the skeleton's rule is not in gallery.css")
	}
	if strings.Contains(app, ".animate-pulse {") {
		t.Fatal("the skeleton's rule is in app.css, which no ordinary page needs")
	}
	if strings.Contains(gallery, "--pk-color-surface-primary:") {
		t.Fatal("gallery.css repeats the tokens")
	}
	if len(ui.Gallery().Fingerprint) != 16 {
		t.Fatalf("the gallery fingerprint is %q", ui.Gallery().Fingerprint)
	}
}

func TestAssetsServeSheetsControllersAndOverlays(t *testing.T) {
	t.Parallel()
	sheet := ui.Compose(design.Default())
	mine := fstest.MapFS{"js/rip.js": {Data: []byte("// rip")}}
	assets := ui.Assets(sheet, mine)
	read := func(name string) string {
		f, err := assets.Open(name)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer f.Close()
		body, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(body)
	}
	if read("app.css") != string(sheet.Body) {
		t.Fatal("app.css is not the composed sheet")
	}
	if read("gallery.css") != string(ui.Gallery().Body) {
		t.Fatal("gallery.css is not the gallery sheet")
	}
	if read("js/rip.js") != "// rip" {
		t.Fatal("the overlay's script is not served")
	}
	for _, name := range ui.Controllers {
		if read("js/"+name) == "" {
			t.Fatalf("%s is empty", name)
		}
	}
	if _, err := assets.Open("js/nothing.js"); err == nil {
		t.Fatal("a file nobody embedded opened")
	}
	if _, err := fs.Stat(assets, "app.css"); err != nil {
		t.Fatalf("stat app.css: %v", err)
	}
}

func TestThereAreFewControllers(t *testing.T) {
	t.Parallel()
	if len(ui.Controllers) > 8 {
		t.Fatalf("there are %d browser controllers; the budget is 8", len(ui.Controllers))
	}
	for _, name := range ui.Controllers {
		if !strings.HasSuffix(name, ".js") {
			t.Fatalf("%q is not a script", name)
		}
	}
}

func TestAClientsOwnPaletteIsTheOnlyThingThatChanges(t *testing.T) {
	t.Parallel()
	mine := design.Default()
	mine.Light.AccentDefault, mine.Dark.AccentDefault = "#ff6900", "#ff6900"
	stock, client := ui.Compose(design.Default()), ui.Compose(mine)
	if stock.Fingerprint == client.Fingerprint {
		t.Fatal("a client's palette left the fingerprint alone")
	}
	a, b := strings.Split(string(stock.Body), "\n"), strings.Split(string(client.Body), "\n")
	if len(a) != len(b) {
		t.Fatalf("the sheets differ in length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] && !strings.Contains(a[i], "--pk-color-") {
			t.Fatalf("line %d differs and is not a token: %q vs %q", i, a[i], b[i])
		}
	}
}

func TestControllersNameNoRoute(t *testing.T) {
	t.Parallel()
	assets := ui.Assets(ui.Compose(design.Default()))
	for _, name := range []string{"session.js", "htmx-config.js", "confirm.js", "theme.js"} {
		body, err := fs.ReadFile(assets, "js/"+name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "/admin") {
			t.Fatalf("%s names a route; the sign-in path is data-signin on <html>", name)
		}
	}
}
