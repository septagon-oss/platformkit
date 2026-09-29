package design_test

import (
	"math"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// The gate cites SC 1.4.3's 0.03928; the pk-design ancestor this is ported from
// branches at sRGB's 0.04045. Neither the port nor the port changed a decision a
// colour could actually observe: an 8-bit channel takes 256 values, and none of
// them falls between the two constants. This test is the reason nobody has to
// re-derive that, and the reason nobody "fixes" the constant to the other one.
func TestWCAGThresholdDoesNotDependOnTheBranchConstant(t *testing.T) {
	t.Parallel()
	linearAt := func(channel, threshold float64) float64 {
		if channel <= threshold {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}
	differing := 0
	for value := range 256 {
		channel := float64(value) / 255
		if linearAt(channel, 0.03928) != linearAt(channel, 0.04045) {
			differing++
		}
	}
	if differing != 0 {
		t.Errorf("%d of 256 channel values land between 0.03928 and 0.04045; the gate's measured ratios would depend on which literal the standard's clause means", differing)
	}
}

// A ratio is a property of two opaque paints. Resolved colours are premultiplied,
// so a translucent foreground reads as a darker opaque one and would clear every
// body role in this package while being unreadable on anything but the surface it
// happened to be measured against — and `transparent`, which parses, measures
// 21:1. The gate therefore refuses alpha before it measures anything.
func TestThemeCheckRefusesAColorCarryingAlpha(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"#15221f80", "#15221f00", "transparent", "#0e1614c0"} {
		for _, theme := range design.Default().Both() {
			theme.TextPrimary = value
			err := theme.Check()
			if err == nil {
				t.Errorf("%s theme with text-primary %q passed the gate", theme.Name, value)
				continue
			}
			if !strings.Contains(err.Error(), "text-primary") || !strings.Contains(err.Error(), "alpha") {
				t.Errorf("refusal of %q names neither the token nor the reason: %v", value, err)
			}
		}
	}
}

// Every colour the kernel ships is an opaque #rrggbb literal, in both themes: the
// gate above is only a gate if nothing shipped slips past it, and the generated
// corpus is checked the same way by TestGeneratedPairsClearTheContrastGate.
func TestEveryShippedColorTokenIsAnOpaqueLiteral(t *testing.T) {
	t.Parallel()
	for _, theme := range design.Default().Both() {
		for _, token := range theme.Tokens() {
			if token.Type != "color" {
				continue
			}
			if !generatedHex.MatchString(token.Value) {
				t.Errorf("%s ships %s = %q, want an opaque #rrggbb literal", theme.Name, token.Name, token.Value)
			}
		}
	}
}

// A client override is the other door: the same value would reach the pair as a
// legible token because alpha is invisible to a ratio. Client.Validate refuses it
// by name, before any pair is generated.
func TestClientValidateRefusesATranslucentOverride(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"#15221f80", "transparent", "#f0b97880"} {
		client := design.Client{
			Slug:   "veil",
			Seed:   design.Seed{Sector: "retail", Name: "Veil"},
			Tokens: map[string]map[string]string{"light": {"text-primary": value}},
		}
		pair, err := client.Resolve()
		if err == nil {
			t.Errorf("a %q foreground resolved to %q", value, pair.Light.TextPrimary)
			continue
		}
		if pair != (design.Pair{}) {
			t.Errorf("a refused override returned a pair anyway")
		}
		if !strings.Contains(err.Error(), "alpha") {
			t.Errorf("refusal of %q does not say why: %v", value, err)
		}
	}
}

// A font stack has one grammar in this package — the one Theme.FontFamilies and
// the export read — so a client may write exactly what the renderer can project.
// Context keywords parse as words and are not stacks; a declaration is not a
// stack either, and the earlier weaker check let both through to explode later.
func TestClientValidateRefusesAStackThePackagesOwnParserRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ field, stack, want string }{
		{"display", "inherit", "must be quoted"},
		{"body", "default", "must be quoted"},
		{"mono", "menu", "must be quoted"},
		{"display", "Georgia, serif; body { display: none }", "unsupported family identifier"},
	} {
		typography := design.Typography{}
		switch tc.field {
		case "display":
			typography.Display = tc.stack
		case "body":
			typography.Body = tc.stack
		case "mono":
			typography.Mono = tc.stack
		}
		client := design.Client{Slug: "type", Seed: design.Seed{Sector: "retail", Name: "Type"}, Typography: typography}
		err := client.Validate()
		if err == nil {
			t.Errorf("%s stack %q validated", tc.field, tc.stack)
			continue
		}
		if !strings.Contains(err.Error(), "font stack that is not one") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s stack %q: got %v, want it to name the stack and say %q", tc.field, tc.stack, err, tc.want)
		}
	}
	// The same test on the side of what a client may write: three stacks this
	// package parses, one quoted name with a comma in it, and a generic fallback.
	for _, stack := range []string{`"Iowan Old Style", Georgia, serif`, "system-ui", `"Times, New Roman", serif`} {
		client := design.Client{Slug: "type", Seed: design.Seed{Sector: "retail", Name: "Type"},
			Typography: design.Typography{Display: stack}}
		if err := client.Validate(); err != nil {
			t.Errorf("a stack the parser reads was refused: %q: %v", stack, err)
		}
	}
}

// A radius reaches the DTCG document a mobile application reads, which carries an
// absolute dimension. A percentage means a different shape per container and is
// inexpressible there; so the grammar is px and rem, and it lives beside the type.
func TestShapeValidateAcceptsALengthAndRefusesEverythingElse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"0.75rem", true}, {"9999px", true}, {"0.375rem", true}, {"1rem", true},
		{"1e3rem", false}, {"-1rem", false}, {"50%", false}, {"calc(1rem + 2px)", false},
		{"12em", false}, {"rem", false}, {"1rem extra", false},
	} {
		shape := design.Shape{CardRadius: tc.value}
		err := shape.Validate()
		if tc.ok && err != nil {
			t.Errorf("radius %q refused: %v", tc.value, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("radius %q accepted, want it refused as not a length in px or rem", tc.value)
		}
	}
	// An unset radius is not a refusal: the token keeps the kernel's own value.
	if err := (design.Shape{}).Validate(); err != nil {
		t.Errorf("an empty shape was refused: %v", err)
	}
}

// The accent is not only a button fill: ui/components paints it as body-size text
// with no background of its own (the brand text utility, the outline and link
// button variants, a brand detail value), so it lands on whichever surface a card
// raised itself onto. This colour clears the two surfaces a button owns and the
// text set on the accent itself, and fails on the third — the pair a page shows
// and the pair the gate used not to read. Refusing it is the point: Client.Resolve
// runs this same Check over a client's named override, so an accent chosen for how
// it looks filled in is refused before it reaches a stylesheet.
func TestThemeCheckRefusesAnAccentLegibleOnlyWhereAButtonPaintsIt(t *testing.T) {
	t.Parallel()
	pair := design.Default()
	pair.Dark.AccentDefault = "#008ce7"
	err := pair.Check()
	if err == nil {
		t.Fatalf("an accent that reads 4.17:1 on the muted surface passed the gate")
	}
	for _, want := range []string{"accent-default", "surface-muted", "4.5:1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if err := design.Default().Check(); err != nil {
		t.Errorf("the pair this repository ships: %v", err)
	}
}
