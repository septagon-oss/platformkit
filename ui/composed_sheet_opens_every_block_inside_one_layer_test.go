package ui_test

// What the composed sheet promises about its own shape: one order statement, and
// every block it opens opened inside one of the four layers. The refusal that keeps
// at-rule text out of a selector's position is a read of the selector, and ui_test.go's
// TestTheGateRefusesASelectorThatIsAnAtRulePreludeAndReadsAnAtSignAsData owns it; this
// file measures the emitted sheet with a brace scanner of its own, because a structural
// fact is not pinned by the reading that writes it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// TestTheComposedSheetStatesOneOrderAndOpensEveryBlockInsideALayer runs over the
// kernel's own sheet and over a sheet with consumer rules in it. A block opened
// outside every layer outranks the order statement, and a
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
