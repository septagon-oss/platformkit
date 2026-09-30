package components_test

// Review round 15 (T-0107). Round 14's fix made a component's edge a measured
// pair: design.edgeRolePairs gates --pk-role-border-primary on
// --pk-role-surface-primary and --pk-role-surface-secondary at
// MinContrastGraphic, and design/roles.go states the scope as "Two grounds,
// because a field is painted on two things". This package paints that same line
// on a third ground, in shipped rules, today:
//
//	clButtonVariant["secondary"] = Bg(style.SurfacePrimary) + BorderColor(style.BorderPrimary)
//		+ On(style.StateHover, hoverBg(style.SurfaceHover))                    // classlists.go:107
//	clModalCancel = Bg(style.SurfacePrimary) + BorderColor(style.BorderPrimary)
//		+ On(style.StateHover, func(…) { return c.Bg(style.SurfaceHover) })     // classlists.go:343
//
// hoverBg sets the control's own fill, so in the hovered state the line's inner
// neighbour is --pk-role-surface-hover — 4 % of text-primary mixed into
// surface-primary, a ground no gated pair names, mixed from two tokens a client
// may both override. The shipped dark pair reads 3.10:1 there, a tenth of a step
// over the floor this branch adopted, and nothing pins it.
//
// The floor is also reachable from a client's own file: border-default #6f6c69
// in a dark theme reads 3.01:1 on the card, which is what the door
// (design.Client.Resolve, the gate design.yaml passes through) requires — and the
// same accepted palette puts the hovered control's edge at 2.71:1 on its own
// fill. A hovered control's border is not the reading SC 1.4.11 asks for in terms
// (a component must be identifiable in its states, and focus carries its own
// indicator), which is why this is a gap in the gate's scope rather than a
// refusal of the shipped sheet. It is still a pair this repository paints, under
// the ratio its own comment says a component's boundary must reach, driven there
// by a file the repository's door accepts.
//
// The case has two ways to pass: gate the ground the hover fill mixes (then the
// door refuses the palette below and there is nothing left to measure), or stop
// painting border-primary under a control whose fill moves out from under it
// (then the witness is not found and the pair is not painted). While neither is
// done it fails, naming the rule, the ground role, the measured ratio and the
// palette it was measured on.

import (
	"os"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// TestShippedEdgeHoldsTheGraphicFloorOnTheGroundsThisPackagePaints measures a
// component's edge on every ground this package paints it on, including the one
// the layer mixes rather than takes from a theme.
func TestShippedEdgeHoldsTheGraphicFloorOnTheGroundsThisPackagePaints(t *testing.T) {
	source, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	hover := paintedHoverGround(string(source), "clButtonVariant")
	if hover == "" {
		hover = paintedHoverGround(string(source), "clModalCancel")
	}
	if hover == "" {
		t.Skip("no shipped rule paints border-primary on a control whose own fill moves to another ground, so the pair is not painted")
	}
	// The card, the page canvas, and the control's own hovered fill.
	grounds := []string{"--pk-role-surface-primary", "--pk-role-surface-secondary", "--pk-role-surface-" + hover}
	t.Logf("measuring the edge on %v (third ground painted by clButtonVariant[\"secondary\"] and clModalCancel)", grounds)

	// One client's own design.yaml: an edge a hundredth of a step over the card
	// floor, which is exactly what the door's floor permits.
	client := design.Client{
		Slug:   "edgehover",
		Seed:   design.Seed{Sector: "logistics", Name: "Meridian Freight"},
		Tokens: map[string]map[string]string{"dark": {"border-default": "#6f6c69"}},
	}
	worn := []edgePalette{{name: "shipped pair", pair: design.Default(), wear: "the kernel's own theme"}}
	resolved, refusal := client.Resolve()
	if refusal != nil {
		worn = append(worn, edgePalette{name: "client file", refused: true, wear: refusal.Error()})
	} else {
		worn = append(worn, edgePalette{name: "client file", pair: resolved, onlyTheme: "dark",
			wear: "design.Client.Resolve accepted a design.yaml whose dark theme names border-default #6f6c69"})
	}

	for _, palette := range worn {
		if palette.refused {
			t.Logf("%s: the door refused it, so nothing reaches a screen: %s", palette.name, palette.wear)
			continue
		}
		for _, theme := range palette.pair.Both() {
			if palette.onlyTheme != "" && theme.Name != palette.onlyTheme {
				continue
			}
			values, resolveErr := design.ResolveColors(theme.Tokens(), design.RoleLayer())
			if resolveErr != nil {
				t.Fatalf("%s/%s: the role layer does not resolve: %v", palette.name, theme.Name, resolveErr)
			}
			edge, okEdge := values["--pk-role-border-primary"]
			if !okEdge {
				t.Fatalf("%s/%s: the layer declares no --pk-role-border-primary", palette.name, theme.Name)
			}
			for _, ground := range grounds {
				surface, okGround := values[ground]
				if !okGround {
					t.Fatalf("%s/%s: the layer declares no %s", palette.name, theme.Name, ground)
				}
				if got := design.Contrast(edge, surface); got < design.MinContrastGraphic {
					t.Errorf("%s/%s: a component's edge measures %.2f:1 on %s, under the %.1f:1 this layer says a component's boundary must reach. clButtonVariant[\"secondary\"] and clModalCancel paint BorderColor(style.BorderPrimary) on a control whose own fill is that ground, no pair in design.GatedRolePairs() names %s, so %s",
						palette.name, theme.Name, got, ground, design.MinContrastGraphic, ground, palette.wear)
				}
			}
		}
	}
}

// edgePalette is one pair and who wears it. refused means the door would not hand
// out the pair at all, which is the other correct outcome.
type edgePalette struct {
	name      string
	pair      design.Pair
	onlyTheme string
	refused   bool
	wear      string
}

// paintedHoverGround reads the ground role one rule paints under a line drawn in
// border-primary when the control's own fill moves, and returns it in the
// declared spelling ("hover" for style.SurfaceHover), or "" when the rule paints
// no such pair. A rule written as a map or as a single declaration is read the
// same way: from its name to the next top-level declaration.
func paintedHoverGround(source, name string) string {
	start := regexp.MustCompile(`(?m)^\s*` + name + `\s*=`).FindStringIndex(source)
	if start == nil {
		return ""
	}
	rest := source[start[1]:]
	if end := regexp.MustCompile("(?m)^\tcl[A-Za-z0-9_]+\\s*=").FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}
	for _, pattern := range []string{
		`BorderColor\(style\.BorderPrimary\)[\s\S]{0,200}?hoverBg\(style\.Surface([A-Za-z0-9]+)\)`,
		`hoverBg\(style\.Surface([A-Za-z0-9]+)\)[\s\S]{0,200}?BorderColor\(style\.BorderPrimary\)`,
	} {
		if found := regexp.MustCompile(pattern).FindStringSubmatch(rest); found != nil {
			return kebabGround(found[1])
		}
	}
	return ""
}

// kebabGround spells a Go constant's suffix the way the role name is written:
// Hover is hover, BrandSoft is brand-soft.
func kebabGround(value string) string {
	out := make([]byte, 0, len(value)+4)
	for i := 0; i < len(value); i++ {
		if c := value[i]; c >= 'A' && c <= 'Z' {
			if i > 0 {
				out = append(out, '-')
			}
			out = append(out, c+('a'-'A'))
			continue
		}
		out = append(out, value[i])
	}
	return string(out)
}
