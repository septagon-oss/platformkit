package ui_test

// The stylesheet has cascade layers. These cases
// read the sheet Compose ships and ask the two questions the delivery's own
// documentation says are answered: does a kernel role rule still outrank the
// utility sitting on its own element, and does the client gate cover every hook
// and every declaration a consumer sheet can carry.
//
// Each case asserts the behaviour the documentation promises, so each has a
// passing branch once the layer placement or the gate is fixed; none asserts
// what the broken sheet prints. The browser half of the first case is measured
// in a browser (a closed modal computes display: flex, a checked
// checkbox computes transparent ink, both hidden at the merge base).

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// layerRanks parses the order statement Compose opens the sheet with.
func layerRanks(t *testing.T, sheet string) map[string]int {
	t.Helper()
	line, _, _ := strings.Cut(sheet, "\n")
	if !strings.HasPrefix(line, "@layer ") || !strings.HasSuffix(line, ";") {
		t.Fatalf("the sheet does not open with an order statement, got %q", line)
	}
	ranks := map[string]int{}
	for i, name := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "@layer "), ";"), ", ") {
		ranks[name] = i
	}
	return ranks
}

// layerOf returns the layer whose block holds the selector, by the
// emitter's own indentation: a layer block opens at column 0 and closes at
// column 0, everything it holds is indented.
func layerOf(t *testing.T, sheet, selector string) string {
	t.Helper()
	at := strings.Index(sheet, selector)
	if at < 0 {
		t.Fatalf("the composed sheet contains no rule %s", selector)
	}
	layer := ""
	for _, line := range strings.Split(sheet[:at], "\n") {
		if strings.HasPrefix(line, "@layer ") && strings.HasSuffix(line, " {") {
			layer = strings.TrimSuffix(strings.TrimPrefix(line, "@layer "), " {")
		} else if line == "}" {
			layer = ""
		}
	}
	if layer == "" {
		t.Fatalf("the rule %s is emitted unlayered, so it beats every layer", selector)
	}
	return layer
}

// TestAKernelRoleRuleIsNotRankedBelowItsOwnUtility is the cascade half of
// the promise. The two pairs are the ties the browser gate measures: the dialog
// element carries the flex utility and the kernel's rule that hides it once it
// closes; the checkbox box carries text-transparent and the kernel's rule that
// paints its ink. An earlier layer loses whatever the selector says, so a role
// rule ranked below the utility on its own element stops applying — a closed
// modal stays on screen and a ticked box shows no mark. Equal rank is enough:
// the role selector outspecifies a single class.
func TestAKernelRoleRuleIsNotRankedBelowItsOwnUtility(t *testing.T) {
	t.Parallel()
	sheet := string(ui.Compose(design.Default()).Body)
	ranks := layerRanks(t, sheet)
	for _, pair := range []struct{ role, utility string }{
		{"dialog[data-component=modal]:not([open]) {", ".flex {"},
		{"[data-component=checkbox] > [data-checkbox-box] {", ".text-transparent {"},
		{"[data-component=checkbox] > input:is(:checked,:indeterminate) + [data-checkbox-box] {", ".bg-surface-primary {"},
	} {
		roleLayer, utilityLayer := layerOf(t, sheet, pair.role), layerOf(t, sheet, pair.utility)
		if ranks[roleLayer] < ranks[utilityLayer] {
			t.Errorf("the kernel role rule %s sits in @layer %s, below the utility %s in @layer %s: the utility wins and the role stops applying",
				pair.role, roleLayer, pair.utility, utilityLayer)
		}
	}
}

// TestComposeRefusesAClientRuleNamingAKernelPart completes the gate. Its
// own comment says the hooks are "the component hook and its internal parts";
// the list carries the checkbox and the modal panel and none of the rest of the
// parts the kernel renders. A consumer rule naming a part the kernel renders is
// the coupling the gate exists to refuse, and the client layer ranks last, so
// such a rule outranks every kernel rule for that element.
func TestComposeRefusesAClientRuleNamingAKernelPart(t *testing.T) {
	t.Parallel()
	for _, hook := range []string{
		"[data-modal-backdrop]", "[data-tabs-panel]", "[data-sidebar-item]", "[data-pagination-next]",
	} {
		sheet := css.NewSheet().Select(hook, css.Decl("display", css.Literal("none")))
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Compose accepted a client rule naming the kernel's own part %s; the gate lists only [data-component, [data-modal-panel], [data-checkbox, [data-theme and :root", hook)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
	}
}

// TestComposeRefusesARawColourInAClientsKeyframes covers the declaration
// the gate never reads. A keyframe is the documented shape of a consumer sheet
// ("a keyframe for an animation"), and its stops are declarations like any
// rule's — the same #hex refused in a rule must be refused in an animation.
func TestComposeRefusesARawColourInAClientsKeyframes(t *testing.T) {
	t.Parallel()
	frames := css.NewSheet().Keyframes("store-pulse", func(k *css.Keyframes) {
		k.At("from", css.Decl("background-color", css.Literal("#ff0000")))
		k.At("to", css.Decl("background-color", css.Literal("#00ff00")))
	})
	body := frames.CSS()
	if !strings.Contains(body, "#ff0000") {
		t.Fatalf("the sheet under test does not carry the raw colour it is meant to: %s", body)
	}
	defer func() {
		if recover() == nil {
			t.Error("Compose accepted a client sheet whose @keyframes carries a raw colour; the same value in a rule is refused")
		}
	}()
	ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{frames}})
}

// TestEveryComposedRuleLivesInALayer pins the claim the whole change
// rests on: no rule is left unlayered, because an unlayered rule beats every
// layer and would undo the order statement. This one passes at this commit; it
// is here so a later consumer sheet, list or base rule that escapes the layers
// fails somewhere.
func TestEveryComposedRuleLivesInALayer(t *testing.T) {
	t.Parallel()
	extra := css.NewSheet().
		Select(".store-hero", css.Decl("color", css.VarRef("pk-color-text-primary", ""))).
		Media("(min-width: 60rem)", func(in *css.Sheet) {
			in.Select(".store-hero", css.Decl("grid-template-columns", css.Literal("2")))
		})
	sheet := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{extra}}).Body)
	for n, line := range strings.Split(sheet, "\n") {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.HasSuffix(line, " {") && !strings.HasPrefix(line, "@layer ") {
			t.Errorf("line %d opens a block outside any layer, so it outranks every layer: %q", n+1, line)
		}
	}
}
