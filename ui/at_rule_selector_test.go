package ui_test

// Review round 7 of T-0108. Two things, one file.
//
// 1. The defect. refuseClientSheet reads the text a rule emits for a brace, a
//    comment start, a kernel attribute name, `:root`, a `--pk-` property and a raw
//    colour — and never asks whether the text is a selector at all. The emitter
//    writes `selector { prop: value; }` (ui/css, Rule.CSS), so text that begins
//    with `@` is not a selector to the browser but an at-rule prelude, and the
//    block after it is the at-rule's body: a consumer sheet that hands Select an
//    at-rule prelude *does* declare an @layer of its own, which is the first thing
//    the gate says it refuses ("a client sheet declares its own @layer; Compose
//    places every client rule in the client layer"). sheet.UsesLayers cannot see
//    it: it walks the at-rules the Sheet type carries, and a prelude arriving in
//    a selector string is not one of those. The three cases below are accepted
//    today and emit an at-rule inside @layer client; each must be refused, by the
//    text it reads, the way the brace case already is. The nested layer gains no
//    rank (it becomes `client.tokens`, inside the client subtree), so this is
//    filed below HIGH — but it is the gate's own first promise, and the sibling of
//    the escape rounds 5 and 6 filed: text that changes what block the emitter is
//    in, arriving where a selector belongs.
//
// 2. The pin. The whole delivery rests on one structural fact nobody measured
//    independently at these bytes: the order statement ranks the four layers, and
//    every block the composed sheet opens is opened inside one of them. That is
//    asserted here from the bytes, with a brace scanner of this file's own — not
//    from a fingerprint, a byte count or a line index, all of which are figures
//    other branches write. It says nothing about which rules are in which layer;
//    ui_test.go and the earlier rounds' files own that.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// TestTheGateRefusesASelectorThatIsAnAtRulePrelude is the finding. A
// selector is a selector: text that opens a block of its own is not one, and the
// gate already refuses the other way a rule's text can move a block.
func TestTheGateRefusesASelectorThatIsAnAtRulePrelude(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		selector string
	}{
		{"a nested layer named tokens", "@layer tokens"},
		{"a nested layer named client", "@layer client"},
		{"a media block with no selector", "@media all"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sheet := css.NewSheet().Select(c.selector,
				css.Decl("color", css.VarRef("pk-color-text-primary", "")))
			refused := func() (out string) {
				defer func() {
					if r := recover(); r != nil {
						out = ""
					}
				}()
				composed := ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
				// The passing branch is the panic above. Reaching the text below
				// means Compose accepted the rule and emitted it, which is the
				// defect; nothing here reads what the defect prints to decide
				// whether to fail.
				body := string(composed.Body)
				start := strings.Index(body, "@layer client {")
				if start < 0 {
					return "the composed sheet has no @layer client block at all"
				}
				return body[start:]
			}()
			if refused == "" {
				return
			}
			t.Errorf("Compose accepted %.24s as a selector and emitted it inside the client layer, so a consumer sheet states an at-rule of its own in the one layer the gate says Compose places it in; the refusal the brace case gets is what this asks for. Emitted: %.240s", c.selector, refused)
		})
	}
}

// TestTheComposedSheetStatesOneOrderAndOpensEveryBlockInsideALayer is this
// round's pin, run over the kernel's sheet and over a sheet with consumer rules
// in it. A block opened outside every layer outranks the order statement, and a
// second order statement would make the sheet's rank whichever document happened
// to load first — either one undoes the delivery in silence, with the sheet still
// looking like four layers to a reader who counts the @layer words.
func TestTheComposedSheetStatesOneOrderAndOpensEveryBlockInsideALayer(t *testing.T) {
	t.Parallel()
	consumer := css.NewSheet().Select(".store-hero",
		css.Decl("color", css.VarRef("pk-color-text-primary", "")))
	for _, c := range []struct {
		name       string
		sheet      string
		wantClient bool
	}{
		{"the kernel's own sheet", string(ui.Compose(design.Default()).Body), false},
		{"a sheet with consumer rules", string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{consumer}}).Body), true},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(c.sheet, "\n")
			if got := strings.TrimSpace(firstNonBlank(lines)); got != "@layer tokens, base, components, client;" {
				t.Fatalf("the sheet's first statement is %q; the one order statement is what makes the ranking the sheet's rather than the document's", got)
			}
			depth := 0
			var topBlocks []string
			var unlayered []string
			for n, line := range lines {
				if trimmed := strings.TrimSpace(line); trimmed == "}" {
					depth--
					continue
				}
				if !strings.HasSuffix(line, "{") {
					continue
				}
				if depth == 0 {
					if !strings.HasPrefix(line, "@layer ") {
						unlayered = append(unlayered, fmt.Sprintf("line %d: %q", n+1, line))
					} else {
						topBlocks = append(topBlocks, strings.TrimSuffix(strings.TrimPrefix(line, "@layer "), " {"))
					}
				}
				depth++
			}
			if depth != 0 {
				t.Fatalf("the brace scan ends at depth %d, so this scanner is reading the sheet wrong", depth)
			}
			if len(unlayered) > 0 {
				t.Errorf("%d blocks open outside every layer and therefore outrank the order statement: %v", len(unlayered), unlayered)
			}
			if want := []string{"tokens", "base", "components", "client"}; !equal(topBlocks, want) {
				t.Errorf("the top-level layer blocks are %v, want %v in that order and one each", topBlocks, want)
			}
			if !c.wantClient {
				return
			}
			client := strings.Index(c.sheet, "@layer client {")
			hero := strings.Index(c.sheet, ".store-hero {")
			if hero < client {
				t.Errorf("the consumer's rule at byte %d lands before @layer client at byte %d, so it is not in the layer the gate reads", hero, client)
			}
		})
	}
}

func firstNonBlank(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
