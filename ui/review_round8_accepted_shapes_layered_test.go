package ui_test

// Review round 8 of T-0108: a pin, not a defect. Rounds 1-7 each pinned a
// refusal; nothing reads what the gate *accepts*. The layer promise dies two
// ways: a consumer rule that escapes @layer client, or a composition that refuses
// a rule a real client writes — and a refused composition panics at mount and
// ships no stylesheet, the failure mode ui/ui.go names itself. So this feeds
// Compose the consumer shapes ui/ui.go's docs and round 20's sweep say the gate
// must take, and counts braces over the bytes back: the blocks opened at depth 0
// are the four layers in that order, no brace opens or closes outside every
// block, and the consumer's text is inside the client one. It reads no refusal
// and asserts no message. Brace counting is exact here because a brace in any
// text the emitter writes is refused, so none reaches a quoted value.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

func TestReviewEveryConsumerShapeTheGateTakesLandsInsideOneLayer(t *testing.T) {
	t.Parallel()
	tok := css.VarRef("pk-color-text-primary", "")
	media := func(s *css.Sheet, q, sel string) {
		s.Media(q, func(in *css.Sheet) { in.Select(sel, css.Decl("color", tok)) })
	}
	for _, c := range []struct {
		name, marker string
		build        func(*css.Sheet)
	}{
		{"one class", ".store-hero {", func(s *css.Sheet) { s.Select(".store-hero", css.Decl("color", tok)) }},
		{"a media block", "(min-width: 40rem) {", func(s *css.Sheet) { media(s, "(min-width: 40rem)", ".store-side") }},
		{"a media in a media", "(min-width: 60rem) {", func(s *css.Sheet) {
			s.Media("(prefers-color-scheme: dark)", func(in *css.Sheet) { media(in, "(min-width: 60rem)", ".store-side") })
		}},
		{"a keyframes block", "@keyframes store-pulse {", func(s *css.Sheet) {
			s.Keyframes("store-pulse", func(k *css.Keyframes) { k.At("from", css.Decl("opacity", css.Literal("0"))) })
		}},
		{"an at-sign in a value", `[data-email="a@b.com"] {`, func(s *css.Sheet) { s.Select(`[data-email="a@b.com"]`, css.Decl("color", tok)) }},
		{"a semicolon in a bracket", `[aria-label*="a;b"] {`, func(s *css.Sheet) { s.Select(`.store-card[aria-label*="a;b"]`, css.Decl("color", tok)) }},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sheet := css.NewSheet()
			c.build(sheet)
			body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
			heads, stray := reviewDepth0Blocks(body)
			if got := strings.Join(heads, "|"); got != "@layer tokens|@layer base|@layer components|@layer client" {
				t.Errorf("with a consumer %s rule in it the sheet opens %v at depth 0, want the four layers in that order: a block outside every layer is unlayered and outranks the order statement", c.name, heads)
			}
			if len(stray) != 0 {
				t.Errorf("the sheet opens or closes a block outside every layer: %q", stray)
			}
			at := strings.LastIndex(body, "@layer client {")
			if at < 0 || !strings.Contains(body[at:], c.marker) {
				t.Errorf("the consumer's %s rule is not inside @layer client", c.marker)
			}
		})
	}
}

// reviewDepth0Blocks returns the head of every block the sheet opens at depth 0
// and every brace there that opened or closed nothing.
func reviewDepth0Blocks(body string) (heads, stray []string) {
	depth, pending := 0, 0
	for at := 0; at < len(body); at++ {
		switch body[at] {
		case '{':
			if depth == 0 {
				heads = append(heads, strings.TrimSpace(body[pending:at]))
			}
			depth++
		case '}':
			if depth == 0 {
				stray = append(stray, strings.TrimSpace(body[pending:at]))
			} else if depth--; depth == 0 {
				pending = at + 1
			}
		case ';':
			if depth == 0 {
				pending = at + 1
			}
		}
	}
	return heads, stray
}
