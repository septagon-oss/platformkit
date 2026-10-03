// rawColourRE covers the CSS Color 4 families, not only a hex triplet and
// rgb()/hsl(). Two things follow that no pin in the tree holds, and one of them is
// a defect.
//
// The first: nine new arms means nine new words the read can fire on. The
// families are `lab`, `lch`, `hwb`, `oklab`, `oklch` and `color`/`color-mix` —
// substrings and path segments of things a consumer sheet legitimately writes
// (`url(/lab/diagram.png)`, `src: url(/fonts/lab.woff2)`, `counter(label)`,
// `print-color-adjust`). The delivery measured those with a throwaway probe that
// it deleted, so the tree today carries no case that a widening of the read —
// the exact change this commit is — must not turn the colour gate into a ban on
// paths, counters and vendor keywords. Case 1 is that floor.
//
// The second: a declaration value is not one token. `url(…)` carries a reference
// and `"…"` carries text, and neither is a colour to a browser however it is
// spelled inside. The read runs over the whole rendered value, so a mask that
// points at an inline SVG filter whose id is hex-shaped is refused as a raw
// colour, with advice that names no token to read. Case 3 asserts the behaviour
// that is correct and does not hold today; it is the reproduction, and it passes
// when the read stops inside a url() and inside a quoted string.
//
// Case 2 is the widening's own reach: a family the read knows must be refused in
// every position a value can sit in — top level, one @media deep, two deep, a
// keyframe stop, and after a `;` in a value, which is the split the read exists
// to make. A cure that adds the families at the top level and loses the nested
// reach would pass the pins written before this commit and fail this one.
package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// composeColourSheet composes one consumer sheet and returns the refusal, or
// "" when the composition took it. The recover is the caller's question, not a
// guard: refuseClientSheet panics by contract, and a composition that refused
// ships no bytes.
func composeColourSheet(t *testing.T, sheet *css.Sheet) (refusal string) {
	t.Helper()
	func() {
		defer func() {
			if r := recover(); r != nil {
				refusal = fmt.Sprint(r)
			}
		}()
		ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
	}()
	return refusal
}

// colourValueRefusal wraps one declaration in the one place a value can sit:
// either bare, one @media deep, two deep, in a keyframe stop, or after a `;`,
// which the gate splits because a browser ends the declaration there.
func colourValueRefusal(t *testing.T, where, property, value string) string {
	t.Helper()
	decl := css.Decl(property, css.Literal(value))
	nested := func(depth int, leaf func(*css.Sheet)) *css.Sheet {
		var build func(in *css.Sheet, left int)
		sheet := css.NewSheet()
		build = func(in *css.Sheet, left int) {
			if left == 0 {
				leaf(in)
				return
			}
			in.Media("(min-width: 1px)", func(inner *css.Sheet) { build(inner, left-1) })
		}
		build(sheet, depth)
		return sheet
	}
	switch where {
	case "top":
		return composeColourSheet(t, css.NewSheet().Select(".store-hero", decl))
	case "media":
		return composeColourSheet(t, nested(1, func(in *css.Sheet) {
			in.Select(".store-hero", decl)
		}))
	case "media twice":
		return composeColourSheet(t, nested(2, func(in *css.Sheet) {
			in.Select(".store-hero", decl)
		}))
	case "keyframe":
		sheet := css.NewSheet()
		sheet.Keyframes("store-hero-in", func(k *css.Keyframes) { k.At("from", decl) })
		return composeColourSheet(t, sheet)
	case "after a ;":
		return composeColourSheet(t, css.NewSheet().Select(".store-hero",
			css.Decl(property, css.Literal("var(--pk-color-accent-default); "+property+": "+value))))
	default:
		t.Fatalf("unknown position %q", where)
		return ""
	}
}

// TestEachNewColourFamilyIsRefusedWhereverAValueSits is the
// widening's reach: the seven families `91a1ada` added, in the five positions a
// value appears in, all refused, and refused as a raw colour rather than by
// whatever neighbouring rule happens to fire.
func TestEachNewColourFamilyIsRefusedWhereverAValueSits(t *testing.T) {
	t.Parallel()
	families := []string{
		"oklch(55% 0.15 250)", "oklab(0.55 0.1 -0.1)", "lab(55% 20 -30)",
		"lch(55% 30 250)", "hwb(250 20% 20%)", "color(display-p3 0.2 0.4 0.8)",
		"color-mix(in oklab, var(--pk-color-accent-default), white 20%)",
	}
	positions := []string{"top", "media", "media twice", "keyframe", "after a ;"}
	checked := 0
	for _, colour := range families {
		for _, where := range positions {
			checked++
			refusal := colourValueRefusal(t, where, "color", colour)
			if refusal == "" {
				t.Errorf("the gate took the raw colour %q %s: the client layer is the sheet's strongest layer, so the palette is spelled there instead of in a token", colour, where)
				continue
			}
			if !strings.Contains(refusal, "raw colour") {
				t.Errorf("the refusal of %q %s does not say it is a colour: %s", colour, where, refusal)
			}
		}
	}
	if checked != len(families)*len(positions) {
		t.Fatalf("this case checked %d compositions, not the %d it names", checked, len(families)*len(positions))
	}
}

// TestTheWidenedReadFiresOnANameAndNotOnAWordShapedLikeOne is the
// floor the nine new arms need and the tree does not have: text that contains a
// family's letters as a path segment, a counter name, a custom-property name or a
// vendor keyword names no colour, so it must keep composing. The fix cannot be a
// ban on the letters.
func TestTheWidenedReadFiresOnANameAndNotOnAWordShapedLikeOne(t *testing.T) {
	t.Parallel()
	for _, use := range []struct{ property, value string }{
		{"background", "url(/lab/diagram.png)"},
		{"background", "url(/color-mix/guide.png)"},
		{"content", "counter(label)"},
		{"content", "attr(data-lab)"},
		{"print-color-adjust", "exact"},
		{"background", "linear-gradient(red, blue)"},
		{"color", "var(--pk-color-accent-default)"},
		{"background", "var(--pk-color-surface) url(/icons/hwb.svg)"},
		{"src", "url(/fonts/lab.woff2)"},
	} {
		sheet := css.NewSheet().Select(".store-hero", css.Decl(use.property, css.Literal(use.value)))
		if refusal := composeColourSheet(t, sheet); refusal != "" {
			t.Errorf("the gate refused a consumer rule that names no colour, %s: %q: %s", use.property, use.value, refusal)
		}
	}
}

// TestAReferenceOrTextThatIsShapedLikeAColourComposes is the
// reproduction of this case's finding. `mask: url(#fade)` points at an element of
// an inline SVG; `content: "#123"` prints the four characters a person typed. A
// browser computes no colour from either, and the gate's promise is about the
// colour the browser reads. The refusal it gets instead tells the developer to
// read a token, which is advice that names nothing they can write.
func TestAReferenceOrTextThatIsShapedLikeAColourComposes(t *testing.T) {
	t.Parallel()
	for _, use := range []struct{ property, value, what string }{
		{"mask", "url(#fade)", "a mask on an inline SVG element whose id is hex-shaped"},
		{"clip-path", "url(#ab12)", "a clip path on an inline SVG element whose id is hex-shaped"},
		{"content", `"#123"`, "text a page prints, not a value a browser computes"},
		{"content", `"color("`, "text that contains a function's letters, not a function call"},
	} {
		sheet := css.NewSheet().Select(".store-hero", css.Decl(use.property, css.Literal(use.value)))
		if refusal := composeColourSheet(t, sheet); refusal != "" {
			t.Errorf("ui.Compose refused %s: %s — %s %q carries no colour, so the raw-colour read refuses a rule the contract leaves the client, and the advice it prints names a token to read where there is none",
				use.what, refusal, use.property, use.value)
		}
	}
}
