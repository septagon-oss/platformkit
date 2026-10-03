package ui_test

// The delivery's own half of the span the raw-colour read steps over. A review
// found the read refusing `mask: url(#fade)` — a pointer to an inline SVG element
// whose id happens to read as six hex digits — and `content: "#123"`, five
// characters a page prints, and the cure blanks the two spans a browser computes
// no value from (outsideValues, ui/ui.go). Three decisions inside that cure belong
// to the delivery rather than to the review, and each is the kind a later change
// reverses quietly, so they are pinned rather than measured with a probe and thrown
// away.
//
// The first is what the blanking must not blind. A blanked span hides its own text
// and nothing else: `url(#ab12) #ff0000` carries a colour the browser computes
// outside the reference, and one part's blanked span does not hide the next part's
// colour across the split on `;` — the split is what the read exists for.
//
// The second is the direction that refuses instead of blanking. A `;` inside a
// quoted string is character data to a browser and a declaration boundary to that
// split, so the part that begins mid-string has no span of its own to blank, and
// blanking to the end of it would hide a colour belonging to a real, later
// declaration. An open span is therefore left exactly as it is, and the case below
// is the colour that shape smuggles.
//
// The third is what blanking does not reach: the `--pk-` read asks only where a part
// begins, so a kernel property after a `;` inside a quoted string stays the
// declaration the split exposed. The sibling case pinned the smuggle through the
// property field of a declaration; this is the same channel through the value, read
// by the read that blanks nothing.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// composeOneValue puts one value on one property of one consumer rule and reports
// the refusal, or "" when the composition took it.
func composeOneValue(property, value string) (refusal string) {
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{
		css.NewSheet().Select(".store-hero", css.Decl(property, css.Literal(value))),
	}})
	return ""
}

func TestABlankedReferenceOrStringDoesNotHideTheColourBesideIt(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"url(#ab12) #ff0000",
		"url(/lab/a.png); color: rgb(1, 2, 3)",
		`"#123"; color: oklch(55% 0.15 250)`,
	} {
		if refusal := composeOneValue("background", value); refusal == "" {
			t.Errorf("Compose took %q: a blanked span hides its own text, and the colour outside it is one the browser computes", value)
		} else if !strings.Contains(refusal, "raw colour") {
			t.Errorf("the refusal of %q does not say it is a colour: %s", value, refusal)
		}
	}
}

func TestAReferenceOrStringLeftOpenStillShowsTheColourAfterIt(t *testing.T) {
	t.Parallel()
	// The string never closes inside this part: the browser reads one string and the
	// `;` as data in it, while the split reads two declarations. Refusing the colour
	// after it is the direction the gate reads in.
	value := `"a;b"; color: #ff0000`
	if refusal := composeOneValue("content", value); refusal == "" {
		t.Errorf("Compose took %q: text after a span the split cut open belongs to a later declaration, where a colour is real", value)
	}
}

func TestTheKernelPropertyReadIsMadeOverTheTextTheColourReadBlanks(t *testing.T) {
	t.Parallel()
	value := `"a;--pk-color-accent-default: currentColor`
	if refusal := composeOneValue("content", value); refusal == "" {
		t.Errorf("Compose took %q: the `--pk-` read asks where a part begins and blanks nothing, so the split on `;` reaches the kernel property inside the string", value)
	}
}

func TestAReferenceAndAStringThatNameNoColourCompose(t *testing.T) {
	t.Parallel()
	for _, use := range []struct{ property, value string }{
		{"background", "url(/brand/lab.png) var(--pk-color-accent-default)"},
		{"mask", "url(#ab12), url(#fade)"},
		{"content", `"color( #ff"`},
	} {
		if refusal := composeOneValue(use.property, use.value); refusal != "" {
			t.Errorf("Compose refused %s: %q, which computes no colour: %s", use.property, use.value, refusal)
		}
	}
}
