package ui_test

// The stylesheet has cascade layers.
//
// refuseClientSheet polices three kernel-owned names: an attribute the kernel
// renders, the root element, and a property in the --pk- namespace (plus the raw
// colour a token is supposed to stand for). The first is read as a name —
// attrNames decodes \NN and \X escapes, folds ASCII case and drops the
// whitespace, and ui/hooks.go states why: "every spelling the reader did not
// parse is a client rule that reaches the client layer, which ranks last". The
// other three are compared to the bytes they are spelled with: a
// strings.Contains on ":root", a regexp on "^\\s*--pk-" and one on
// "#[0-9a-fA-F]{3,8}|(rgb|hsl)a?\\(". CSS resolves all three the way it
// resolves an attribute name, so the same sentence is a hole in each of them.
//
// Measured in this repository's own Chromium (v. the one `make e2e` drives) with
// the real ui.Compose(design.Default()) sheet linked, an extra <style> carrying
// one more @layer client block, and a swatch of the kernel's own
// .bg-surface-brand (components layer) whose background-color is
// var(--pk-role-surface-brand), which the tokens layer defines on :root as
// var(--pk-color-accent-default) = #0f5d4e:
//
// 	no client rule                   -> background-color: rgb(15, 93, 78)
// 	:ROOT { --\70 k-color-…: rgb(255,0,0) } -> rgb(255, 0, 0)   ← accepted by the gate
// 	.rgb-upper { color: RGB(1,2,3) }         -> color: rgb(1, 2, 3)  ← accepted
// 	.rgb-upper { color: #\36 666 }           -> color: rgba(102, 102, 102, .4) ← accepted
//
// The accepted case repaints every kernel component on the page: the client
// layer ranks after tokens, the rule lands on the root element, var() is
// substituted where the role is declared, and the property name decodes to
// --pk-color-accent-default. That is the inversion the layer order exists to
// prevent, reached with a sheet the gate let through. ui_test.go pins the plain
// spellings; nothing pinned these.
//
// Each case below asserts what the fixed gate does — refuse, and name the key it
// refused — so every case passes once the read canonicalizes the name the way
// attrNames already does, and the second test pins that the cure does not refuse
// what merely reads a token.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// composeClientRule composes one consumer rule and returns the gate's
// refusal, or "" when Compose accepted the sheet and emitted it.
func composeClientRule(t *testing.T, selector, property, value string) (refusal string) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			refusal = fmt.Sprint(p)
		}
	}()
	sheet := css.NewSheet().Select(selector, css.Decl(property, css.Literal(value)))
	ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
	return ""
}

func TestTheGateRefusesEverySpellingTheBrowserResolves(t *testing.T) {
	for _, c := range []struct {
		name                      string
		selector, property, value string
		key                       string // what the refusal must name
	}{
		// The spellings the gate already refuses, so the cure cannot be a
		// regression of these into acceptance.
		{":root as written", ":root", "background", "white", "root"},
		{"--pk- as written", ".store-hero", "--pk-color-accent-default", "white", "pk-color-accent-default"},
		{"#fff as written", ".store-hero", "color", "#fff", ""},
		{"[DATA-COMPONENT] as written", "[DATA-COMPONENT=button]", "background", "white", "data-component"},

		// A pseudo-class name is ASCII case-insensitive and a CSS identifier
		// decodes escapes: all four address the root element, which the gate
		// says it refuses ("that markup is the kernel's to style").
		{":ROOT upper", ":ROOT", "background", "white", "root"},
		{":RoOt mixed", ":RoOt", "background", "white", "root"},
		{":root with an escaped o", ":ro\\6f t", "background", "white", "root"},
		{":root with an escaped R", ":\\52 OOT", "background", "white", "root"},

		// A custom property name decodes escapes too, so this declares
		// --pk-color-accent-default, which the gate says is "the kernel's to
		// name". Beside :ROOT it repaints the whole palette (see above).
		{"--pk- with an escaped p", ".store-hero", "--\\70 k-color-accent-default", "white", "pk-color-accent-default"},
		{"--pk- with an escaped p, upper", ".store-hero", "--\\70 K-color-accent-default", "white", "pk-color-accent-default"},
		{"--pk- after a semicolon, escaped", ".store-hero", "color", "white;--\\70 k-color-accent-default:crimson", "pk-color-accent-default"},
		// The pair the browser measurement above is made of, as one rule. It is
		// refused here only by accident — the value it was measured with carries
		// rgb(), which rawColourRE sees before either name read is reached — so
		// the case is stated with a colour name, which the gate says it cannot
		// tell from a keyword, and asserts only that the gate refuses it.
		{"the whole override, as measured", ":ROOT", "--\\70 k-color-accent-default", "white", ""},

		// A colour function name is ASCII case-insensitive, and a hash token
		// decodes escapes: both are colours to the browser, and the gate
		// refuses a raw colour so the palette is named in one place.
		{"RGB() upper", ".store-hero", "color", "RGB(1, 2, 3)", ""},
		{"hsl() upper", ".store-hero", "color", "HSL(120, 50%, 50%)", ""},
		{"#hex with an escaped digit", ".store-hero", "color", "#\\36 6666", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			refusal := composeClientRule(t, c.selector, c.property, c.value)
			if refusal == "" {
				t.Fatalf("ui.Compose accepted the consumer rule %s { %s: %s }, which a browser reads as the kernel's own: it refused nothing, so the sheet ships the rule inside @layer client, which ranks after the layer that names it",
					c.selector, c.property, c.value)
			}
			if !strings.HasPrefix(refusal, "ui: ") {
				t.Fatalf("the refusal is not the client-sheet gate's: %s", refusal)
			}
			if c.key != "" && !strings.Contains(strings.ToLower(refusal), strings.ToLower(c.key)) {
				t.Fatalf("the refusal does not name %q, so a consumer cannot tell which of its rules the gate means: %s", c.key, refusal)
			}
		})
	}
}

// TestTheGateStillTakesWhatOnlyReadsAKernelName is the other half: the
// cure for the spellings above must not turn the gate into a refusal of every
// sheet that mentions a kernel word. A client layer reads the palette by name —
// that is what it is for — and owns its own hooks and its own custom properties.
func TestTheGateStillTakesWhatOnlyReadsAKernelName(t *testing.T) {
	for _, c := range []struct {
		name                      string
		selector, property, value string
	}{
		{"reading a token", ".store-hero", "color", "var(--pk-color-accent-default)"},
		{"reading a token in a role", ".store-hero", "background-color", "var(--pk-role-surface-brand)"},
		{"a custom property of its own", ".store-hero", "--store-hero-height", "4rem"},
		{"a hook of its own", "[data-store-card=featured]", "gap", "1rem"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if refusal := composeClientRule(t, c.selector, c.property, c.value); refusal != "" {
				t.Fatalf("the gate refused a rule that names nothing it owns: %s", refusal)
			}
		})
	}
}
