package ui_test

// The client gate reads several text forms, each named for what it does to a
// block. This file asks the question all of them exist to keep true, of the bytes
// a composition emits: every block the sheet opens sits inside one of the four
// layers, and every block opened inside @layer client is one the emitter owns — a
// @media or @keyframes the consumer asked the API for, never an at-rule its own.
// The scan that came before skipped every indented line, so a block a consumer stated through
// selector text passed it — refused at the head read
// (atRuleHead in ui/ui.go) and pinned here as structure. Each row says whether
// Compose must refuse it, so the file fails when the gate narrows or widens.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

func TestAConsumerSheetThatComposesOpensNoBlockTheEmitterDidNotOwn(t *testing.T) {
	t.Parallel()
	rule := func(selector string) func(*css.Sheet) {
		return func(s *css.Sheet) {
			s.Select(selector, css.Decl("color", css.VarRef("pk-color-text-primary", "")))
		}
	}
	decl := func(property, value string) func(*css.Sheet) {
		return func(s *css.Sheet) { s.Select(".store-hero", css.Decl(property, css.Literal(value))) }
	}
	media := func(query, selector string) func(*css.Sheet) {
		return func(s *css.Sheet) {
			s.Media(query, func(m *css.Sheet) { m.Select(selector, css.Decl("opacity", css.Literal("1"))) })
		}
	}
	keyframes := func(name, offset string) func(*css.Sheet) {
		return func(s *css.Sheet) {
			s.Keyframes(name, func(k *css.Keyframes) { k.At(offset, css.Decl("opacity", css.Literal("0"))) })
		}
	}
	for _, c := range []struct {
		name    string
		refused bool
		build   func(*css.Sheet)
	}{
		{"a class rule", false, rule(".store-hero")},
		{"an attribute value carrying @", false, rule(".store-hero[data-email=a@b.com]")},
		{"a semicolon inside a url()", false, decl("background-image", "url(https://x/a;b)")},
		{"a @media the consumer asked for", false, media("(min-width: 60rem)", ".store-hero")},
		{"a @keyframes the consumer asked for", false, keyframes("store-pulse", "from")},
		{"@layer tokens as a selector", true, rule("@layer tokens")},
		{"@layer client as a selector", true, rule("@layer client")},
		{"@media all as a selector", true, rule("@media all")},
		{"an escaped at-keyword name", true, rule("@\\6c ayer tokens")},
		{"@LAYER upper-cased", true, rule("@LAYER tokens")},
		{"a selector that ends its rule", true, rule(".store-hero; @layer base")},
		{"a @media query that is one", true, media("@layer tokens", ".store-hero")},
		{"a rule nested in a @media", true, media("screen", "@layer tokens")},
		{"a @keyframes name that is one", true, keyframes("@layer tokens", "from")},
		{"a keyframe offset with a ;", true, keyframes("store-pulse", "from; @layer tokens")},
		{"a brace inside a value", true, decl("background-image", "none}body{position:fixed}")},
		{"a @layer block of its own", true, func(s *css.Sheet) { s.Layer("client", func(*css.Sheet) {}) }},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sheet := css.NewSheet()
			c.build(sheet)
			var body, refusal string
			func() {
				defer func() {
					if r := recover(); r != nil {
						refusal = fmt.Sprint(r)
					}
				}()
				body = string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
			}()
			if c.refused {
				if refusal == "" {
					t.Errorf("Compose accepted this, and the sheet it emits breaks: %v", layerBreaks(body))
				}
				return
			}
			if refusal != "" {
				t.Fatalf("Compose refused CSS a consumer may write: %.160s", refusal)
			}
			if breaks := layerBreaks(body); len(breaks) > 0 {
				t.Errorf("the sheet Compose emits breaks: %v", breaks)
			}
		})
	}
}

// layerBreaks reads composed bytes with a brace count of its own. The emitter puts
// one brace on a line — at the end of a head or alone on a closing line — so a line
// carrying one elsewhere holds text that opened or closed a block of its own. The
// four layer blocks must be the only blocks at depth zero, and no at-rule may open
// inside @layer client beyond the two the API hands a consumer.
func layerBreaks(body string) []string {
	var breaks, top []string
	depth, clientAt := 0, -1
	for n, line := range strings.Split(body, "\n") {
		if !strings.ContainsAny(line, "{}") {
			continue
		}
		if strings.TrimSpace(line) == "}" {
			if depth == clientAt {
				clientAt = -1
			}
			depth--
			continue
		}
		if !strings.HasSuffix(line, "{") || strings.ContainsRune(line, '}') || strings.Count(line, "{") != 1 {
			breaks = append(breaks, fmt.Sprintf("line %d carries a brace the emitter did not write: %.70s", n+1, line))
			continue
		}
		head := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		if depth == 0 {
			top = append(top, head)
			if !strings.HasPrefix(head, "@layer ") {
				breaks = append(breaks, fmt.Sprintf("line %d opens %q outside every layer", n+1, head))
			}
		}
		own := strings.HasPrefix(head, "@") && !strings.HasPrefix(head, "@media ") && !strings.HasPrefix(head, "@keyframes ")
		switch {
		case depth == 0 && head == "@layer client":
			clientAt = depth + 1
		case clientAt >= 0 && own:
			breaks = append(breaks, fmt.Sprintf("line %d opens the at-rule %q inside @layer client", n+1, head))
		}
		depth++
	}
	if depth != 0 {
		breaks = append(breaks, fmt.Sprintf("the brace count ends at depth %d", depth))
	}
	if got := strings.Join(top, "|"); got != "@layer tokens|@layer base|@layer components|@layer client" {
		breaks = append(breaks, fmt.Sprintf("the top-level blocks are [%s]", got))
	}
	return breaks
}
