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
		"border-width: 0",             // width utilities opt in without accidental borders
		"border-style: solid",         // CSS otherwise defaults to an invisible border
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

func TestComposeEmitsTheFourLayersInOrder(t *testing.T) {
	t.Parallel()
	sheet := css.NewSheet().Select(".a", css.Decl("color", css.Literal("red")))
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
	statement := "@layer tokens, base, components, client;"
	if !strings.HasPrefix(body, statement) {
		t.Fatalf("the sheet must open with the order statement, got:\n%.80s", body)
	}
	prev := 0
	for _, block := range []string{"@layer tokens {", "@layer base {", "@layer components {", "@layer client {"} {
		at := strings.Index(body, block)
		if at <= prev {
			t.Fatalf("the %s block is missing or out of order", block)
		}
		prev = at
	}
}

// TestAConsumerRuleCannotDisplaceAComponentRule pins the emission the layer
// promise rests on: a consumer's bytes land inside the client layer and nowhere
// else, so no consumer rule moves, duplicates or precedes a kernel utility's
// rule in the kernel's own layer.
//
// What the order statement then says, stated the right way round: for normal
// declarations a LATER layer wins over an earlier one whatever the selector
// (CSS Cascade Layers, §6), which is why the client layer is last — a client
// restyling its own markup is the point of it — and why a !important reverses
// that, which is where the kernel's own overrides live. What protects a kernel
// component is therefore not the layer order but the gate: a client rule is
// refused before it reaches a layer if it names a kernel hook, the root element
// or a --pk- property (TestComposeRefusesConsumerSheetsThatEscapeTheClientLayer),
// and a kernel role rule shares the components layer with the utilities on its
// own element so its selector still decides (ui/review_round1_layers_test.go).
func TestAConsumerRuleCannotDisplaceAComponentRule(t *testing.T) {
	t.Parallel()
	// The same declaration the .flex utility makes, on a doubled selector:
	// higher specificity than any kernel rule, and the one selector a client may
	// legitimately write because .flex is a class, not a kernel hook.
	hijack := css.NewSheet().Select(".flex.flex", css.Decl("display", css.Literal("inline")))
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{hijack}}).Body)
	client := strings.Index(body, "@layer client {")
	if client < 0 {
		t.Fatal("there is no client layer")
	}
	at := strings.Index(body, ".flex.flex {")
	if at < client {
		t.Fatal("a consumer rule was emitted outside the client layer")
	}
	if strings.Contains(body[:client], ".flex.flex {") {
		t.Fatal("a consumer rule reached a kernel layer")
	}
}

func TestComposeRefusesConsumerSheetsThatEscapeTheClientLayer(t *testing.T) {
	t.Parallel()
	hidden := css.NewSheet()
	hidden.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select("[data-component=card]", css.Decl("display", css.Literal("none")))
	})
	for name, sheet := range map[string]*css.Sheet{
		"a kernel component hook": css.NewSheet().Select("[data-component=button]", css.Decl("border", css.Literal("0"))),
		"a modal panel":           css.NewSheet().Select("[data-modal-panel]", css.Decl("margin", css.Literal("0"))),
		"the theme attribute":     css.NewSheet().Select(`[data-theme="dark"]`, css.Decl("color-scheme", css.Literal("light"))),
		"the root element":        css.NewSheet().Select(":root", css.Decl("--pk-color-accent-default", css.Literal("#00f"))),
		"a kernel property":       css.NewSheet().Select(".a", css.Decl("--pk-role-surface-brand", css.Literal("teal"))),
		"a raw colour":            css.NewSheet().Select(".a", css.Decl("color", css.Literal("#ff0000"))),
		"a raw rgb()":             css.NewSheet().Select(".a", css.Decl("background", css.Literal("rgb(0 0 0 / 0.45)"))),
		"a role inside @media":    hidden,
		"a layer of its own":      css.NewSheet().Layer("client", func(in *css.Sheet) { in.Select(".a", css.Decl("color", css.Literal("red"))) }),
		"a colour in a keyframe": css.NewSheet().Keyframes("store-pulse", func(k *css.Keyframes) {
			k.At("from", css.Decl("background-color", css.Literal("#ff0000")))
		}),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted %s", name)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
	}
}

func TestComposeKeepsConsumerRulesAfterSharedUtilities(t *testing.T) {
	t.Parallel()
	// Carried from the round that predates cascade layers, when this asserted
	// that a consumer's override of [data-component=button] survived after the
	// utilities. That override is now refused outright (see the refusal test),
	// which supersedes surviving after it; what still must hold is the two
	// facts the old case pinned: the consumer's bytes are not folded into or
	// moved before a shared utility's rule, and composing leaves the input
	// sheet untouched. The hooks are the consumer's own, since a kernel role
	// selector no longer reaches this far.
	extra := css.NewSheet().
		Select(".store-hero-button", css.Decl("border", css.Literal("1px solid var(--pk-color-accent-default)"))).
		Select(".store-hero-card", css.Decl("box-shadow", css.Literal("inset 0 1px 2px var(--pk-color-border-strong)")))
	before := extra.CSS()
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{extra}}).Body)
	for _, pair := range [][2]string{{".border-transparent {", ".store-hero-button {"}, {".shadow {", ".store-hero-card {"}} {
		utility, override := strings.Index(body, pair[0]), strings.LastIndex(body, pair[1])
		if utility < 0 || override <= utility {
			t.Fatalf("consumer rule %s must remain after utility %s", pair[1], pair[0])
		}
	}
	if extra.CSS() != before {
		t.Fatal("composing the application changed consumer declarations")
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
	if !strings.HasPrefix(gallery, "@layer components {") {
		t.Fatal("gallery.css is not placed in the components layer")
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
