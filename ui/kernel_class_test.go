package ui_test

// Review round 11 of T-0108 (the stylesheet has cascade layers). One question,
// asked of the gate rather than of the emitter: the brief states the rule a
// client lives under as "a client sheet is `@layer client` only, `var()` only,
// and a gate refuses a client rule that names a kernel role's own selector",
// and *Done when* names the test — "a test proves a client rule cannot beat a
// component rule".
//
// The gate refuses the kernel's *attribute* vocabulary by name, decoded and
// folded (`attrNames`, `kernelHooks`). It does not refuse its *class*
// vocabulary, which is the other half of how the kernel's markup is addressed —
// the class lists in `ui/components` are what `ui/style` resolves and what
// `Compose` emits into the components layer, and `emitted` already computes that
// set to deduplicate it. A browser resolves a class selector the same way it
// resolves an attribute name, so the same read reaches it: `class="flex"` is
// matched by `.flex` and by `.\66 lex` alike.
//
// The consequence is measured, not argued. Over the real `ui.Compose` sheet in
// this repository's own Chromium (`chrome-headless-shell` from
// ~/.cache/ms-playwright/chromium_headless_shell-1243, `--headless --dump-dom`
// of a page whose <style> is the composed bytes), each sheet below is accepted
// and its rule wins over the kernel's own rule for the same element, because the
// client layer is ranked last and a later layer beats an earlier one for normal
// declarations whatever the selector:
//
//	no consumer rule .......... modal.display=none | flex.display=flex | sr.position=absolute
//	dialog, .flex, .sr-only ... modal.display=block | flex.display=inline | sr.position=static
//
// The three are the failures this branch's own comments say the layer placement
// exists to prevent: `componentState` moved `dialog[data-component=modal]:not([open])`
// out of `base` because "a dismissed modal keeps covering the page", and
// `[data-checkbox-box]` because "a ticked box paints no mark". A client sheet
// that names `dialog` rather than `[data-modal-panel]` reintroduces the first,
// and a sheet that names `.sr-only` makes the kernel's screen-reader-only text
// visible on every page that application serves.
//
// Each case below asserts the behaviour the brief and the gate's own comment
// promise — a refusal, naming the class it refused — and has a passing branch
// once the gate reads class names as it reads attribute names. Nothing here
// reads what the accepted sheet prints to decide whether to fail: the premise
// that the class is the kernel's own is checked against the sheet Compose emits,
// which Compose emits today.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// reviewRound11KernelSheet is the composed sheet with one consumer rule in it,
// composed so a panic can be reported rather than crash the suite.
func reviewRound11KernelSheet(t *testing.T, sheets ...*css.Sheet) (body string, refusal string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	body = string(ui.Compose(design.Default(), ui.Extra{Sheets: sheets}).Body)
	return body, ""
}

// reviewRound11LayerOf returns the layer whose block holds selector, by the
// emitter's own indentation: a layer block opens and closes at column 0.
func reviewRound11LayerOf(t *testing.T, sheet, selector string) string {
	t.Helper()
	at := strings.Index(sheet, selector)
	if at < 0 {
		return ""
	}
	layer := ""
	for _, line := range strings.Split(sheet[:at], "\n") {
		if strings.HasPrefix(line, "@layer ") && strings.HasSuffix(line, " {") {
			layer = strings.TrimSuffix(strings.TrimPrefix(line, "@layer "), " {")
		} else if line == "}" {
			layer = ""
		}
	}
	return layer
}

// TestTheGateRefusesAConsumerRuleThatNamesAKernelClass is the brief's third
// rule and its *Done when*: a client rule that names a kernel role's own selector
// is refused. The kernel's role selectors are not only attributes — `.flex`,
// `.sr-only` and `.bg-surface-brand` are emitted by Compose itself, out of the
// class lists `ui/components` declares, and are the selectors the kernel's markup
// carries.
func TestTheGateRefusesAConsumerRuleThatNamesAKernelClass(t *testing.T) {
	for _, c := range []struct {
		name     string
		selector string // what the consumer writes
		kernel   string // a selector the composed sheet carries in its own layer
		key      string // what a refusal must name
		impact   string
	}{
		{
			name: "a layout utility every kernel component carries",
			// Measured: every .flex on the page computes display:inline.
			selector: ".flex", kernel: ".flex {", key: "flex",
			impact: "flex.display=inline where the kernel renders flex",
		},
		{
			name: "the screen-reader-only helper",
			// Measured: the kernel's visually hidden text computes position:static.
			selector: ".sr-only", kernel: ".sr-only {", key: "sr-only",
			impact: "sr.position=static, the hidden text of every page on screen",
		},
		{
			name:     "a role utility, the class round 10's finding was about",
			selector: ".bg-surface-brand", kernel: ".bg-surface-brand {", key: "bg-surface-brand",
			impact: "every element painted by the brand surface role",
		},
		{
			name: "the same class spelled with an escape",
			// `.\66 lex` resolves to `.flex`, and matches class="flex" in
			// Chromium; class names, unlike attribute names, are case-sensitive,
			// so this is the escape spelling that means the same class.
			selector: `.\66 lex`, kernel: ".flex {", key: "flex",
			impact: "the same rule the plain spelling makes",
		},
		{
			name:     "one selector in a list",
			selector: ".store-hero, .sr-only", kernel: ".sr-only {", key: "sr-only",
			impact: "one selector of the list reaches the kernel's rule",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			sheet := css.NewSheet().Select(c.selector, css.Decl("display", css.Literal("block")))
			before, _ := reviewRound11KernelSheet(t)
			if before == "" {
				t.Fatal("the kernel's own sheet does not compose")
			}
			// The premise, checked rather than assumed: this class is the
			// kernel's own, emitted in a kernel layer ahead of the client layer.
			if reviewRound11LayerOf(t, before, c.kernel) != "components" {
				t.Fatalf("the case's premise is wrong: %s is not emitted in @layer components", c.kernel)
			}
			body, refusal := reviewRound11KernelSheet(t, sheet)
			if refusal == "" {
				at := strings.Index(body, "@layer client {")
				t.Errorf("ui.Compose accepted a consumer rule that names the kernel's own selector %q: it is emitted at byte %d of a %d-byte sheet, inside @layer client, which ranks last, so %s. The brief requires the gate to refuse a client rule that names a kernel role's own selector, and the gate refuses the kernel's attribute names by exactly this read; a class name is as much a name to a browser, and `emitted` already computes the set of them.",
					c.selector, at, len(body), c.impact)
				return
			}
			if !strings.HasPrefix(refusal, "ui: ") {
				t.Errorf("the refusal is not prefixed `ui: `: %q", refusal)
			}
			if !strings.Contains(refusal, c.key) {
				t.Errorf("the refusal does not name the class it refused (%q): %q", c.key, refusal)
			}
		})
	}
}

// TestTheGateStillTakesAConsumerRuleThatNamesOnlyItsOwnClass is the cure's
// other half, green today and required to stay green: the refusal reads the
// kernel's own class vocabulary, and a consumer that names a class only its own
// markup carries composes as it does now. A cure that refuses every class
// selector would pass the case above by shutting the client layer, which is what
// the layer exists to hold.
func TestTheGateStillTakesAConsumerRuleThatNamesOnlyItsOwnClass(t *testing.T) {
	sheet := css.NewSheet().
		Select(".store-hero", css.Decl("display", css.Literal("grid"))).
		Select(".store-hero-card", css.Decl("padding", css.Literal("1rem"))).
		Select(".store-hero", css.Decl("color", css.VarRef("pk-color-accent-default", "")))
	body, refusal := reviewRound11KernelSheet(t, sheet)
	if refusal != "" {
		t.Fatalf("Compose refused a consumer rule that names no kernel class: %q", refusal)
	}
	client := strings.Index(body, "@layer client {")
	if client < 0 {
		t.Fatal("the composed sheet carries no client layer")
	}
	for _, want := range []string{".store-hero {", ".store-hero-card {"} {
		if at := strings.Index(body, want); at < client {
			t.Errorf("%q is emitted at byte %d, ahead of the client layer at %d", want, at, client)
		}
	}
}
