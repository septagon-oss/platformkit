package ui_test

import (
	"fmt"
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

// TestTheGateRefusesWhatABrowserActsOnAndNotACharacter pins the other side of
// the same refusal: the gate reads the text a browser would act on, so a value
// that merely contains a punctuation mark a stylesheet writes elsewhere is not a
// escape. A data: URL carries a semicolon and brackets in a string; a var()
// read carries the kernel's own property name. Both are the consumer's to write,
// and a cure that refused them would refuse the layer instead of the escape.
func TestTheGateRefusesWhatABrowserActsOnAndNotACharacter(t *testing.T) {
	t.Parallel()
	grain := css.NewSheet().Select(".store-card",
		css.Decl("background-image", css.Literal(`url("data:image/svg+xml;base64,AAAA")`)),
		css.Decl("border", css.Literal("1px solid var(--pk-color-accent-default)")))
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{grain}}).Body)
	client := strings.Index(body, "@layer client {")
	if client < 0 {
		t.Fatal("there is no client layer")
	}
	for _, want := range []string{`url("data:image/svg+xml;base64,AAAA")`, "var(--pk-color-accent-default)"} {
		if !strings.Contains(body[client:], want) {
			t.Errorf("the client layer lost %s", want)
		}
	}
}

// TestAConsumerRuleCannotDisplaceAComponentRule pins the emission the layer
// promise rests on: a consumer's bytes land inside the client layer and nowhere
// else, so no consumer rule moves, duplicates or precedes a kernel utility's
// rule in the kernel's own layer. Naming one is the other half of the promise,
// and it is a refusal rather than an emission: the client layer ranks last, so a
// rule addressed at a class the kernel emits would win the kernel's own element
// whatever its layer says, which is why the gate stops it before it reaches one.
//
// What the order statement then says, stated the right way round: for normal
// declarations a LATER layer wins over an earlier one whatever the selector
// (CSS Cascade Layers, §6), which is why the client layer is last — a client
// restyling its own markup is the point of it — and why a !important reverses
// that, which is where the kernel's own overrides live. What protects a kernel
// component is therefore not the layer order but the gate: a client rule is
// refused before it reaches a layer if it names a kernel hook, one of the
// kernel's own classes, the root element or a --pk- property
// (TestComposeRefusesConsumerSheetsThatEscapeTheClientLayer),
// and a kernel role rule shares the components layer with the utilities on its
// own element so its selector still decides (ui/kernel_role_rule_ranked_below_utility_test.go).
func TestAConsumerRuleCannotDisplaceAComponentRule(t *testing.T) {
	t.Parallel()
	// The declaration the .flex utility makes, on a doubled selector: higher
	// specificity than any kernel rule, and a name the gate has no business
	// refusing, because no markup the kernel renders carries it. An earlier
	// version of this case wrote the selector as .flex.flex on the assertion that
	// a class is not a kernel hook; that is false of the classes ui/components
	// declares, and the case below refuses it.
	hijack := css.NewSheet().Select(".store-hero.store-hero", css.Decl("display", css.Literal("inline")))
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{hijack}}).Body)
	client := strings.Index(body, "@layer client {")
	if client < 0 {
		t.Fatal("there is no client layer")
	}
	at := strings.Index(body, ".store-hero.store-hero {")
	if at < client {
		t.Fatal("a consumer rule was emitted outside the client layer")
	}
	if strings.Contains(body[:client], ".store-hero.store-hero {") {
		t.Fatal("a consumer rule reached a kernel layer")
	}
	for _, names := range []string{".flex", ".flex.flex", ".sr-only", ".bg-surface-brand"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted a consumer rule on %s: the class is the kernel's own and the client layer ranks last, so that rule displaces the component's own for the element the kernel renders the class on", names)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{
				css.NewSheet().Select(names, css.Decl("display", css.Literal("inline")))}})
		}()
	}
}

func TestComposeRefusesConsumerSheetsThatEscapeTheClientLayer(t *testing.T) {
	t.Parallel()
	hidden := css.NewSheet()
	hidden.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select("[data-component=card]", css.Decl("display", css.Literal("none")))
	})
	outside := css.NewSheet()
	outside.Media("(min-width: 1px)}@layer base{body", func(in *css.Sheet) {
		in.Select(".store-card", css.Decl("color", css.VarRef("pk-color-fg-primary", "")))
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
		// The same escapes written as text rather than as typed rules: a brace in
		// the emitted text of a rule ends the block Compose opened around it, so
		// what follows is no longer inside the client layer at all — and an
		// unlayered rule beats every layer the order statement names.
		"a brace in a selector":         css.NewSheet().Select(".store-card}.grain", css.Decl("color", css.Literal("red"))),
		"a brace in a value":            css.NewSheet().Select(".grain", css.Decl("background-image", css.Literal("none}body{position:fixed}"))),
		"a comment in a value":          css.NewSheet().Select(".grain", css.Decl("background-image", css.Literal("none /*"))),
		"a brace in an at-rule prelude": outside,
		// The browser ends a declaration at a semicolon, so the text after one is
		// a second declaration with a property of its own — in either field the
		// emitter writes into. The gate reads the emitted text, not the fields.
		"a kernel property inside a value":      css.NewSheet().Select(".grain", css.Decl("color", css.Literal("red;--pk-color-accent-default:currentColor"))),
		"a kernel property in a property field": css.NewSheet().Select(".grain", css.Decl("color:transparent;--pk-color-accent-default", css.Literal("currentColor"))),
		"a raw colour in a property field":      css.NewSheet().Select(".grain", css.Decl("color:#0f5d4e;outline-color", css.Literal("currentColor"))),
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

// TestTheGateRefusesTheClassAttributeByValueAndItsPresenceByNoName is both edges
// of one read. Refused: a comparison of `class`'s contents is the class namespace
// addressed by value, and the gate refuses names rather than reachability, so it
// refuses the spelling whatever value is compared — including a word no kernel
// rule styles, which no browser would match to kernel markup. Accepted: `[class]`
// asks only whether the element carries the attribute, and every element does, so
// that selector names nothing and reaches what `*` reaches, which is the limit
// refuseClientSheet states rather than a vocabulary it guards. Pinning both sides
// is what keeps the edge a decision: a later change that read values for kernel
// names only would refuse less than the sentence above promises, and one that
// refused presence would refuse a selector no more dangerous than the universal
// selector every client may write.
func TestTheGateRefusesTheClassAttributeByValueAndItsPresenceByNoName(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{`[class~="store-hero"]`, `[class*="grain"]`, `[class] [class$="x"]`} {
		grain := css.NewSheet().Select(selector, css.Decl("color", css.Literal("var(--pk-color-text-primary)")))
		func() {
			defer func() {
				refusal := recover()
				if refusal == nil {
					t.Errorf("Compose accepted %s, which compares the contents of the class attribute", selector)
					return
				}
				if reason := fmt.Sprint(refusal); !strings.Contains(reason, "class") {
					t.Errorf("the refusal of %s does not name the attribute it refused: %q", selector, reason)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{grain}})
		}()
	}
	for _, selector := range []string{`[class]`, `.store-hero[class], .store-card`} {
		grain := css.NewSheet().Select(selector, css.Decl("color", css.Literal("var(--pk-color-text-primary)")))
		body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{grain}}).Body)
		if !strings.Contains(body, "@layer client {") {
			t.Errorf("the sheet for %s carries no client layer", selector)
		}
	}
}

// TestTheGateRefusesAConsumerRuleAtAClassACompositionOwnsListCarries is the same
// refusal at the boundary ui/components does not reach. A consumer hands Compose
// Extra.Lists, and they resolve into @layer components beside the components' own
// — that is what gives a shared utility one rule — so a class that enters the
// kernel's layer by that road is a class a consumer rule can outrank from the
// client layer exactly as .sr-only is. The vocabulary therefore comes from the
// composition, not from one package's declaration list: the premise reads the
// sheet this composition emits, and the second half is that the same rule
// composes when no list in the sheet carries the name, because a sheet that
// states no rule for a class leaves that class to whoever wrote it.
func TestTheGateRefusesAConsumerRuleAtAClassACompositionOwnsListCarries(t *testing.T) {
	t.Parallel()
	title := style.New().Tracking(style.TrackingTight)
	grain := func() *css.Sheet {
		return css.NewSheet().Select(".tracking-tight", css.Decl("letter-spacing", css.Literal("0")))
	}()
	served := string(ui.Compose(design.Default(), ui.Extra{Lists: []style.ClassList{title}}).Body)
	if !strings.Contains(served, ".tracking-tight {") {
		t.Fatalf("premise: the sheet this composition emits carries no rule at .tracking-tight, so this case would refuse a rule about a class nothing styles")
	}
	if layer := reviewRound11LayerOf(t, served, ".tracking-tight {"); layer != "components" {
		t.Fatalf("premise: .tracking-tight is emitted in layer %q, not the components layer the client layer outranks", layer)
	}
	func() {
		defer func() {
			refusal := recover()
			if refusal == nil {
				t.Error("Compose took a consumer rule at a class its own Extra.Lists carry into @layer components")
				return
			}
			if reason := fmt.Sprint(refusal); !strings.Contains(reason, "tracking-tight") {
				t.Errorf("the refusal does not name the class it refused: %q", reason)
			}
		}()
		ui.Compose(design.Default(), ui.Extra{Lists: []style.ClassList{title}, Sheets: []*css.Sheet{grain}})
	}()
	if body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{grain}}).Body); !strings.Contains(body, ".tracking-tight {") {
		t.Errorf("the gate refused a rule at a class no rule of this sheet styles: the client layer carries none and the components layer should not")
	}
}

// TestTheGateRefusesAKernelWordBesideAConsumersOwnClass pins the scope of the
// attribute refusal: it reads names, not reachability. `.store-card` is the
// consumer's own class and `data-state` is the kernel's word (ui/components
// renders it on a checkbox, a modal and a media player), so the selector can
// only ever match the consumer's element — and is refused for the name it
// mentions. Refusing the name is the deliberate choice refuseClientSheet
// documents, so this case is what makes changing it visible: narrowing the gate
// to "selectors that could reach a kernel element" would refuse less than the
// documentation promises, and widening it to whole selectors would refuse a
// client for a word a browser never matched to kernel markup.
func TestTheGateRefusesAKernelWordBesideAConsumersOwnClass(t *testing.T) {
	t.Parallel()
	grain := css.NewSheet().Select(".store-card[data-state=featured]",
		css.Decl("border", css.Literal("1px solid var(--pk-color-accent-default)")))
	defer func() {
		refusal := recover()
		if refusal == nil {
			t.Fatal("Compose accepted a client rule naming data-state beside the client's own class")
		}
		for _, want := range []string{"data-state", ".store-card[data-state=featured]", "ui/components"} {
			if reason := fmt.Sprint(refusal); !strings.Contains(reason, want) {
				t.Errorf("the refusal %q does not name %s", reason, want)
			}
		}
	}()
	ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{grain}})
}

// TestTheGateRefusesASelectorThatIsAnAtRulePreludeAndReadsAnAtSignAsData is both
// sides of the selector read. Refused: text that occupies the position a selector
// occupies is tokenised as an at-rule prelude, and the block after it is the
// at-rule's body, so a rule whose head is `@layer tokens` states a layer of its
// own inside the client layer — refused at the head of the list and at the head
// of any comma-separated part. Accepted: an `@` the browser already reads inside a
// token — an attribute value, or an escape, where `\` is what makes an ident token
// and never an at-keyword. Over-refusal is the expensive side of a gate: a sheet
// refused at mount panics at composition and the consumer ships no stylesheet at
// all, so the read that refuses a prelude must be a read of selector positions and
// not a search for a byte. The two at-rule preludes below the selector loop are
// the same read at the two positions besides a selector where the emitter writes
// caller text ahead of a brace: it prefixes the `@media` and `@keyframes` keyword itself, so a
// query or name that begins with `@layer` states a second at-rule where the
// emitter wrote one, with no `;` involved — the shape TestTheGateRefusesASemicolon…
// reaches only through a semicolon.
func TestTheGateRefusesASelectorThatIsAnAtRulePreludeAndReadsAnAtSignAsData(t *testing.T) {
	t.Parallel()
	rule := func(selector string) *css.Sheet {
		return css.NewSheet().Select(selector,
			css.Decl("color", css.VarRef("pk-color-text-primary", "")))
	}
	for _, selector := range []string{"@layer tokens", "@media all", "  @layer client", ".store-hero, @layer base", `@import "http://x/y.css"`} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted %.40q as a selector: its head is an at-keyword, so the text after it is the body of a block Compose did not open and the client rule leaves the layer the gate reads", selector)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{rule(selector)}})
		}()
	}
	for name, sheet := range map[string]*css.Sheet{
		"a @media query that is a layer": css.NewSheet().Media("@layer tokens", func(m *css.Sheet) {
			m.Select(".store-hero", css.Decl("opacity", css.Literal("1")))
		}),
		"a @keyframes name that is a layer": css.NewSheet().Keyframes("@layer tokens", func(k *css.Keyframes) {
			k.At("from", css.Decl("opacity", css.Literal("0")))
		}),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted %s: the emitter writes this text ahead of the `{` it supplies, so the at-keyword inside it names a block the consumer stated inside the client layer", name)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
	}
	for _, selector := range []string{`[data-email="a@b.com"] .store-hero`, `\40 layer tokens`, `.store-hero, .store-grain`} {
		composed := ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{rule(selector)}})
		if !strings.Contains(string(composed.Body), selector+" {") {
			t.Errorf("the consumer rule on %q was not composed: the refusal reads the head of each selector, not every byte a rule carries", selector)
		}
	}
}

// TestTheGateRefusesASemicolonWhereTheEmitterWritesABrace is the other half of the
// head read. A `;` in the text written ahead of a `{` ends the rule the emitter
// started — a browser does not read further — so whatever follows is the head of
// the *next* rule, read inside the block the emitter closed. Four positions carry
// that text: a selector, a keyframe offset, a @media query and a @keyframes name,
// and each one below states an at-rule Compose did not place, in the layer whose
// first promise is that a consumer states none. None of it gains rank (a nested
// @layer inside client is the sublayer client.tokens, ranked inside the client
// subtree), which is why the refusal is about the promise and not about a win. The
// control is the semicolon a browser cannot act on — inside a bracketed attribute
// value, inside a url() — and the one inside a declaration, which is a second
// declaration and belongs to the read above, not to this one.
func TestTheGateRefusesASemicolonWhereTheEmitterWritesABrace(t *testing.T) {
	t.Parallel()
	for name, sheet := range map[string]*css.Sheet{
		"a selector that ends its own rule": css.NewSheet().Select(".store-card; @layer tokens",
			css.Decl("color", css.VarRef("pk-color-text-primary", ""))),
		"a keyframe offset": css.NewSheet().Keyframes("store-pulse", func(k *css.Keyframes) {
			k.At("from; @layer tokens", css.Decl("opacity", css.Literal("0")))
		}),
		"a @media query": css.NewSheet().Media("all; @layer tokens", func(m *css.Sheet) {
			m.Select(".store-card", css.Decl("color", css.VarRef("pk-color-text-primary", "")))
		}),
		"a @keyframes name": css.NewSheet().Keyframes("spin; @layer tokens", func(k *css.Keyframes) {
			k.At("from", css.Decl("opacity", css.Literal("0")))
		}),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted %s: the browser reads the `;` as the end of the rule Compose was writing, so the @layer after it is one the consumer stated inside the client layer", name)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
	}
	legal := css.NewSheet().
		Select(`.store-card[data-email="a;b@c.com"]`, css.Decl("background-image",
			css.Literal(`url(data:image/svg+xml;charset=utf8,%3csvg/%3e)`))).
		Media("(min-width: 40rem)", func(m *css.Sheet) {
			m.Select(".store-hero", css.Decl("opacity", css.Literal("1")))
		})
	body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{legal}}).Body)
	for _, want := range []string{`[data-email="a;b@c.com"]`, "charset=utf8", "@media (min-width: 40rem) {"} {
		if !strings.Contains(body, want) {
			t.Errorf("the composed sheet lost %q: a `;` inside a quoted attribute value or a url() is character data in a token the browser already opened, so the refusal reads the position ahead of the brace and not the byte", want)
		}
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

// TestEveryClientSheetRefusalSaysWhyInItsMessage pins the shape of the refusal,
// not its reach. refuseClientSheet is the one rule a consumer sheet lives under,
// ten paths refuse under it, and they report through one panic — a composition
// is wired at mount, so the panic text is what reaches the developer whose sheet
// tripped it and what a recovered mount logs. The refusals above each check that
// their own case refuses; this one checks that none of them refuses in silence:
// every path names the package that refused and then says what it read, because
// a gate that hands back a token, a sentinel or an empty string refuses the same
// rules and diagnoses nothing, and the ten share the one print where that would
// happen.
func TestEveryClientSheetRefusalSaysWhyInItsMessage(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		sheet *css.Sheet
	}{
		{"a @layer of its own", css.NewSheet().Layer("client", func(in *css.Sheet) {
			in.Select(".store-hero", css.Decl("color", css.Literal("red")))
		})},
		{"a brace in a value", css.NewSheet().Select(".store-grain",
			css.Decl("background-image", css.Literal("none}body{position:fixed}")))},
		{"a comment start in a value", css.NewSheet().Select(".store-grain",
			css.Decl("background-image", css.Literal("none /*")))},
		{"an at-rule prelude as a selector", css.NewSheet().Select("@layer tokens",
			css.Decl("color", css.VarRef("pk-color-text-primary", "")))},
		{"a semicolon at a selector head", css.NewSheet().Select(".store-card; @layer base",
			css.Decl("color", css.VarRef("pk-color-text-primary", "")))},
		{"a style-element close in a value", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal("</style><img src=x>")))},
		{"the root element", css.NewSheet().Select(":root",
			css.Decl("color-scheme", css.Literal("light")))},
		{"a kernel hook", css.NewSheet().Select("[data-component=button]",
			css.Decl("border", css.Literal("0")))},
		{"a kernel class", css.NewSheet().Select(".sr-only",
			css.Decl("position", css.Literal("static")))},
		{"a --pk- property", css.NewSheet().Select(".store-hero",
			css.Decl("--pk-role-surface-brand", css.Literal("teal")))},
		{"a raw colour", css.NewSheet().Select(".store-hero",
			css.Decl("color", css.Literal("#ff0000")))},
	} {
		t.Run(c.name, func(t *testing.T) {
			var refusal any
			func() {
				defer func() { refusal = recover() }()
				ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{c.sheet}})
			}()
			if refusal == nil {
				t.Fatalf("Compose accepted %s, so there is no refusal here to read a reason from", c.name)
			}
			msg, ok := refusal.(string)
			if !ok || !strings.HasPrefix(msg, "ui: ") || len(msg) <= len("ui: ") {
				t.Errorf("the refusal of %s says nothing a consumer can act on: %#v — a refusal is a panic at mount, and its text is the only diagnosis there is", c.name, refusal)
			}
		})
	}
}
