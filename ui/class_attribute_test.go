package ui_test

// Review round 12 of T-0108. One question, aimed at the cure round 11 asked and
// round 12 shipped: `classNames` reads the classes a selector addresses with a
// `.`, decoded through `scanIdent`/`scanEscape`. A class name is also addressable
// as the *value* of the `class` attribute — `[class~="sr-only"]`, `[class*="sr-only"]`,
// `[class="sr-only"]` — and to the selector engine that value is the same token
// the `.sigil` carries: it matches exactly the elements `@layer components` styles
// at `.sr-only`. `attrNames` reads the attribute's *name* (`class`, which the
// kernel renders no hook under) and never its value, and `classNames` never sees
// the text because the scan steps over a bracket for the reason its comment
// gives — inside one, a `.` is character data. So the second spelling of the same
// name reaches @layer client, the layer that outranks every kernel rule.
//
// This is not the type-selector limit the gate's comment states: `dialog` names
// neither namespace and reaching it needs a claim about what a tag can match.
// `[class~="sr-only"]` states the kernel's class name verbatim, needs no claim at
// all, and refuses nothing a client owns — a client styles its own classes with
// `.store-hero`, not by matching the `class` attribute's contents. `git grep "\[class"`
// over this repository at HEAD answers no sheet at all, so the refusal costs
// nothing here.
//
// Nothing below reads the accepted sheet to decide whether to fail. The premise
// (the class is the kernel's own, emitted in @layer components, and refused in the
// `.name` spelling) is checked against the sheet Compose emits today; the
// assertion is the refusal the brief demands, reached through the refusal's own
// text.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// reviewRound12Compose composes a consumer sheet into the kernel's sheet and
// returns the served bytes, or the refusal if the gate refused it.
func reviewRound12Compose(t *testing.T, sheets ...*css.Sheet) (body string, refusal string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	return string(ui.Compose(design.Default(), ui.Extra{Sheets: sheets}).Body), ""
}

// reviewRound12ConsumerRule is a rule a consumer would actually write: its own
// selector, a token read as a value, no brace, no comment, no raw colour.
func reviewRound12ConsumerRule(selector string) *css.Sheet {
	s := css.NewSheet()
	s.Select(selector, css.Decl("position", css.Literal("static")))
	return s
}

// TestTheGateRefusesAConsumerRuleThatReachesAKernelClassByAttributeValue is
// the second spelling of the name round 11 closed the first spelling of. The
// premise runs first and passes today: the class is the kernel's own, Compose
// emits a rule for it inside @layer components, and the `.name` spelling of the
// same name is already refused. What follows is the same name as an attribute
// value — which the browser resolves to the same elements and the gate reads as
// nothing but the `class` attribute.
func TestTheGateRefusesAConsumerRuleThatReachesAKernelClassByAttributeValue(t *testing.T) {
	t.Parallel()
	kernel, _ := reviewRound12Compose(t)
	for _, class := range []string{"sr-only", "flex"} {
		if got := reviewRound11LayerOf(t, kernel, "."+class+" {"); got != "components" {
			t.Fatalf("premise: .%s is emitted in layer %q, not in the components layer the client layer outranks", class, got)
		}
		if _, refusal := reviewRound12Compose(t, reviewRound12ConsumerRule("."+class)); refusal == "" {
			t.Fatalf("premise: the gate takes a consumer rule at .%s, which round 11 closed; this case is about the other spelling of a name the gate still reads", class)
		}
	}
	for _, tc := range []struct{ selector, class string }{
		{`[class~="sr-only"]`, "sr-only"},
		{`[class~="flex"]`, "flex"},
		{`[class*="sr-only"]`, "sr-only"},
		{`[class^="sr-only"]`, "sr-only"},
		{`[class$="sr-only"]`, "sr-only"},
		{`[class|="sr-only"]`, "sr-only"},
		{`[class="flex"]`, "flex"},
		{`[class~=sr-only]`, "sr-only"},
		{`[\63 lass~="sr-only"]`, "sr-only"},
		{`.store-hero [class~="sr-only"]`, "sr-only"},
	} {
		body, refusal := reviewRound12Compose(t, reviewRound12ConsumerRule(tc.selector))
		if refusal == "" {
			t.Errorf("ui.Compose accepted a consumer rule at %s: the browser matches it against the elements it gives the class %q, which @layer components styles, and @layer client ranks last, so the rule wins the kernel's own element; the sheet carries it at %d bytes", tc.selector, tc.class, len(body))
			continue
		}
		if body != "" {
			t.Errorf("a refused composition (%s) still returned %d bytes of sheet", tc.selector, len(body))
		}
		if !strings.Contains(refusal, "class") {
			t.Errorf("the refusal of %s does not name what it refused (%q read as %q)", tc.selector, refusal, "class")
		}
	}
}

// TestTheGateStillTakesAConsumerRuleThatMatchesItsOwnAttribute is the other
// half of the cure: reading a class value out of `class=` is what is refused, not
// the attribute selector itself. A consumer rule addressed at its own markup by
// its own attribute, or at a kernel word that sits in some other attribute's
// value, is the client layer's own business and must still compose.
func TestTheGateStillTakesAConsumerRuleThatMatchesItsOwnAttribute(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		`[data-email="a.b"]`,
		`.store-card[aria-label*=".flex"]`,
		`.store-hero[data-store-region="featured"]`,
		`.store-hero [data-store-panel]`,
	} {
		if body, refusal := reviewRound12Compose(t, reviewRound12ConsumerRule(selector)); refusal != "" {
			t.Errorf("ui.Compose refused a consumer rule that names no kernel class and no kernel attribute (%s): %s", selector, refusal)
		} else if !strings.Contains(body, "@layer client {") {
			t.Errorf("the sheet for %s carries no client layer", selector)
		}
	}
}
