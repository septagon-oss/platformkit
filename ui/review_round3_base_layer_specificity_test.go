package ui_test

// Review round 3 of T-0108 (the stylesheet has cascade layers). Round 1's HIGH
// was one rule in the wrong layer: `dialog[data-component=modal]:not([open])`
// sat in @layer base, the `flex` utility sat in @layer components, a layer ranks
// before specificity, and a dismissed modal kept covering the page. The cure
// moved that rule and its siblings into componentState() in @layer components.
//
// Nothing in the tree keeps the *shape* of that bug from coming back. The two
// reviewer files and ui_test.go name the rules that moved and check those; a
// later change that writes a new rule about a component into base() — the place
// rules about markup used to live — composes, passes every one of them, and ships
// the same regression for a component nobody thought to list.
//
// So this case pins the contract ui/ui.go's own documentation states for base():
// "every selector is an element or a universal selector, so a class on the
// element outspecifies it in any layer". Any selector in @layer base that carries
// attribute, class, id or pseudo-class specificity outranks nothing it is allowed
// to be overruled by — and, sitting in the layer *ahead* of the class lists, is
// outranked by any one class sitting on the element it governs. The case reads
// only this package's composed output, so it measures this branch, not the tree.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// reviewBaseLayerBlock returns the body of @layer base { … } as the emitter
// writes it: the block opens at column 0 and closes at column 0.
func reviewBaseLayerBlock(t *testing.T, sheet string) string {
	t.Helper()
	const open = "@layer base {"
	at := strings.Index(sheet, open)
	if at < 0 {
		t.Fatalf("the composed sheet carries no @layer base block:\n%.120s", sheet)
	}
	rest := sheet[at+len(open):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatal("the @layer base block is never closed at column 0")
	}
	return rest[:end]
}

// reviewCarriesOwnSpecificity reports whether a selector names more than elements
// and pseudo-elements: an attribute selector, a class, an id or a pseudo-class
// (a single colon; "::before" is a pseudo-element and stays element-level). Those
// are exactly the forms that reach past a preflight rule, so they belong in the
// layer the utilities live in — see componentState.
func reviewCarriesOwnSpecificity(selector string) string {
	for i := 0; i < len(selector); i++ {
		switch selector[i] {
		case '[':
			return "an attribute selector"
		case '.', '#':
			if i+1 < len(selector) && (isNameByte(selector[i+1])) {
				if selector[i] == '.' {
					return "a class selector"
				}
				return "an id selector"
			}
		case ':':
			if i+1 < len(selector) && selector[i+1] == ':' {
				i++ // "::before" is a pseudo-element: element-level specificity
				continue
			}
			if i+1 < len(selector) && isNameByte(selector[i+1]) {
				return "a pseudo-class"
			}
		}
	}
	return ""
}

func isNameByte(c byte) bool {
	return c == '-' || c == '_' || ('0' <= c && c <= '9') ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func TestReviewNothingThatNamesAComponentIsEmittedInTheBaseLayer(t *testing.T) {
	t.Parallel()
	sheet := string(ui.Compose(design.Default()).Body)
	block := reviewBaseLayerBlock(t, sheet)

	// The matcher checks itself, so a case that passes on an empty block or on a
	// parser that read nothing cannot be mistaken for a green pin.
	if got := reviewCarriesOwnSpecificity("[data-component=button]"); got == "" {
		t.Fatal("the specificity matcher reads an attribute selector as an element selector")
	}
	for _, control := range []string{".flex", "#pk-root", "input:focus-visible", "dialog:has(> input)"} {
		if reviewCarriesOwnSpecificity(control) == "" {
			t.Errorf("the specificity matcher reads %q as an element selector", control)
		}
	}
	for _, allowed := range []string{"*, *::before, *::after", "dialog::backdrop", "nav ul, nav ol", "html"} {
		if got := reviewCarriesOwnSpecificity(allowed); got != "" {
			t.Errorf("the specificity matcher calls %q %s; a preflight selector is allowed to be an element or a universal selector", allowed, got)
		}
	}

	checked := 0
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "@") || !strings.HasSuffix(trimmed, "{") {
			continue // a declaration, a comment, or an at-rule's query
		}
		for _, selector := range strings.Split(strings.TrimSuffix(trimmed, "{"), ",") {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				continue
			}
			checked++
			if kind := reviewCarriesOwnSpecificity(selector); kind != "" {
				t.Errorf("the composed sheet emits %q in @layer base, and %s is more than an element selector: a layer ranks before specificity, so this rule is outranked by any single utility class on the element it governs — the shape of the rule this branch moved out of base() to fix a dismissed dialog that never closed. Write it in componentState() instead.", selector, kind)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("only %d selectors were read out of @layer base; the block is not being parsed: %s", checked, block)
	}
}
