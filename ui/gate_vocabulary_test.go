package ui_test

// One question about the client gate: does it cover
// the kernel's own markup? The answer widened the vocabulary to
// components.Hooks and matched the attribute *name* an attribute selector
// addresses. These cases test the cure against the two things it did not cover:
// the parts the kernel renders outside ui/components, and the selector forms a
// browser accepts besides the one spelling the regular expression reads.
//
// Every case asserts the behaviour ui/ui.go's documentation promises ("A consumer
// gets one layer it may write and no way to name a kernel hook"), so each has a
// passing branch once the gate covers the name whatever its form. The last case
// is the pin of the part that is fixed: it passes today and fails the moment the
// component-state rules move out of the layer their own selectors live in.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// acceptsClientSheet reports whether Compose placed the sheet instead of
// refusing it. The refusal is a panic, so the case is decided by what Compose
// does, not by what it prints.
func acceptsClientSheet(t *testing.T, sheet *css.Sheet) (accepted bool) {
	t.Helper()
	defer func() {
		accepted = recover() == nil
	}()
	ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
	return true
}

// TestTheClientGateRefusesAKernelHookInEverySelectorFormABrowserReads pins how
// wide the attribute read has to be. The gate compares the attribute name as
// written, while CSS allows whitespace inside the brackets and around the operator
// and escapes inside an identifier, and HTML matches attribute names
// ASCII-case-insensitively. A read of one spelling leaves the rest open, and a
// browser reads every spelling below as the same hook, so the list the gate reads
// is every form a browser resolves — whitespace inside the brackets, escapes inside
// the name, a capitalised name, a quoted value — not the one spelling anybody types.
//
// The browser half is measured in a browser: with the sheet Compose
// emits for `[ data-component=button ]`, a real components.Button computes
// border-radius 77px instead of the 6px `[data-component=button]` gives it from
// --pk-radius-button, because the client layer ranks after the components layer.
func TestTheClientGateRefusesAKernelHookInEverySelectorFormABrowserReads(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		"[data-component=button]",             // the spelling the gate reads today
		"[ data-component=button ]",           // CSS allows whitespace inside the brackets
		"[data-component = button ]",          // and around the operator
		"[DATA-COMPONENT=button]",             // HTML attribute names match case-insensitively
		`[data-component="button"]`,           // a quoted value is the same coupling
		"[data-component=button]:has(> span)", // a complex selector naming the hook
		`[\64 ata-component="button"]`,        // \64 is "d": the identifier the browser reads
		"[data\\2D component=\"button\"]",     // \2D is "-": the same name, escaped
	} {
		sheet := css.NewSheet().Select(selector, css.Decl("border-radius", css.Literal("77px")))
		if acceptsClientSheet(t, sheet) {
			t.Errorf("Compose accepted a client rule whose selector %s names data-component, a hook ui/components renders: the client layer ranks after the components layer, so this rule restyles a kernel component", selector)
		}
	}
}

// TestTheClientGateCoversTheKernelPartsRenderedOutsideComponents asks the
// same question of the kernel's reach rather than of one selector. components.Hooks
// is the vocabulary of one
// package, and its own test refuses that package a name the list omits; the
// kernel also renders markup in the page shell (ui/document), in the screens it
// generates (ui/resource) and in the gallery (ui/export). A client sheet reaches
// all of it through the same client layer.
func TestTheClientGateCoversTheKernelPartsRenderedOutsideComponents(t *testing.T) {
	t.Parallel()
	for _, part := range []struct{ hook, renderedBy string }{
		{"[data-signin]", "ui/document/document.go, on the document of every page"},
		{"[data-principal]", "ui/document/document.go, on the same document"},
		{"[data-session-error]", "ui/document/document.go, the error the shell renders"},
		{"[data-request-notice]", "ui/document/document.go, the notice a response swaps in"},
		{"[data-confirm]", "ui/resource/resource.go, the delete action of a generated screen"},
		{"[data-confirm-label]", "ui/resource/resource.go, the same action's label"},
	} {
		sheet := css.NewSheet().Select(part.hook, css.Decl("display", css.Literal("none")))
		if acceptsClientSheet(t, sheet) {
			t.Errorf("Compose accepted a client rule naming %s, which %s renders: a client can hide the shell's own affordance and the layer carries it past every kernel rule", part.hook, part.renderedBy)
		}
	}
}

// layerBlockAt returns the layer whose block contains the byte offset, by
// the emitter's own indentation: a layer block opens and closes at column 0.
func layerBlockAt(sheet string, at int) string {
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

// TestAComponentStateRuleSharesTheComponentsLayerAheadOfTheClassLists is
// the pin of the cure. The role rule must share the
// components layer with the utility on its own element — a layer ranks before
// specificity, so an earlier layer loses whatever the selector says — and must be
// emitted ahead of the class lists, so the equal-specificity ties a component's
// own markup creates answer the way they answered before layers existed. The
// preflight stays in the base layer, where any class outspecifies it.
//
// This one passes today. It is here because both halves are
// load-bearing and nothing else in the tree reads the order inside the layer:
// moving componentState() after rules(), or back into base(), is silent until a
// dismissed dialog stays on the viewport.
func TestAComponentStateRuleSharesTheComponentsLayerAheadOfTheClassLists(t *testing.T) {
	t.Parallel()
	sheet := string(ui.Compose(design.Default()).Body)
	if _, _, ok := strings.Cut(sheet, "@layer tokens, base, components, client;\n"); !ok {
		t.Fatal("the sheet does not open with the order statement")
	}
	componentsBlock := strings.Index(sheet, "@layer components {")
	if componentsBlock < 0 {
		t.Fatal("the sheet carries no components layer")
	}
	client := strings.Index(sheet, "@layer client {")
	if client < componentsBlock {
		t.Fatalf("the client layer block is placed before the components layer block")
	}
	lists := strings.Index(sheet, ".flex {")
	if lists < componentsBlock {
		t.Fatal("the resolved class lists are not in the components layer")
	}
	for _, role := range []string{
		"dialog[data-component=modal]:not([open]) {",
		"[data-component=checkbox] > [data-checkbox-box] {",
		"[data-component=button] {",
	} {
		at := strings.Index(sheet, role)
		if at < 0 {
			t.Fatalf("the composed sheet carries no rule %s", role)
		}
		if got := layerBlockAt(sheet, at); got != "components" {
			t.Errorf("the kernel role rule %s sits in @layer %q, not @layer components: a utility class on the same element outranks it", role, got)
			continue
		}
		if at > lists {
			t.Errorf("the kernel role rule %s is emitted after the resolved class lists inside @layer components, so every equal-specificity tie flips against the kernel from the answer it had before layers existed", role)
		}
	}
	for _, preflight := range []string{"html {", "body {", "table {"} {
		at := strings.Index(sheet, preflight)
		if at < 0 {
			t.Fatalf("the composed sheet carries no preflight rule %s", preflight)
		}
		if got := layerBlockAt(sheet, at); got != "base" {
			t.Errorf("the preflight rule %s sits in @layer %q, not @layer base: it now outranks the utility classes it exists to be overruled by", preflight, got)
		}
	}
}

// TestTheClientGateReadsAKeyframeNestedInAMediaBlock pins the cure at the depth the
// walk now reaches: css.WalkRules visits a
// @keyframes stop because the browser applies its declarations, and it recurses
// through a nested at-rule, so the same colour hidden inside a @media block is
// refused too. Nothing else in the tree composes the two together.
func TestTheClientGateReadsAKeyframeNestedInAMediaBlock(t *testing.T) {
	t.Parallel()
	sheet := css.NewSheet()
	sheet.Media("(prefers-reduced-motion: no-preference)", func(in *css.Sheet) {
		in.Keyframes("store-pulse", func(k *css.Keyframes) {
			k.At("50%", css.Decl("background-color", css.Literal("#ff0000")))
		})
	})
	if body := sheet.CSS(); !strings.Contains(body, "#ff0000") {
		t.Fatalf("the sheet under test does not carry the raw colour it is meant to: %s", body)
	}
	if acceptsClientSheet(t, sheet) {
		t.Error("Compose accepted a raw colour in a @keyframes stop nested inside a @media block; the same colour in a rule and in a bare keyframe is refused")
	}
}
