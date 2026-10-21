package internal

// Review round 17 (T-0107). Rounds 14, 15 and 16 each found a ground a shipped
// rule paints a component's edge on that the one contrast gate did not name —
// first the card and the canvas, then the fill a hovered control moves under its
// own line, then the muted panel — and each cure was a pair added to
// design.EdgeRolePairs. Round 16's pin reads the shipped rules back against that
// gate, and it is the right instrument, but its sweep is scoped to its own
// package: r16Sources is ui/components/{classlists,sidebar_disclosure}.go.
//
// This module draws edges too. clHeader paints BorderColor(style.BorderPrimary)
// under its own fill and clFooter paints it on the page's ground, in the shipped
// sheet every page of this application serves. Today those two grounds are
// surface-primary and surface-secondary — both gated, both clearing 3:1 in every
// palette round 16 measured — so nothing is broken here as this file is written.
// What is fragile is that nothing checks it: a future edit that moves clHeader's
// rule onto the filled brand panel (where border-primary reads 1.30:1 in the
// shipped corpus) would draw an edge no client could see, the gate would refuse
// no palette, round 16's pin would stay green from the other package, and a door
// that refuses nobody is the finding every earlier round filed.
//
// So this case asks of this module's own file the same two things round 16 asks
// of ui/components:
//
//   - every ground this module paints a border-primary edge on is named by
//     design.EdgeRolePairs for that border role, so the door (design.Client.
//     Resolve, the gate design.yaml passes through) refuses a client whose
//     palette would put the line under the floor; and
//   - the edge measures at least MinContrastGraphic on its ground in every
//     palette a client can actually be issued — the shipped pair and a
//     generated corpus, both themes.
//
// Its way to pass is the way it has always been: name the pair in
// design.EdgeRolePairs, or stop drawing the line on that ground. A border drawn
// in the layer's decorative hairline (border-secondary) fails here rather than
// riding the exemption round 16 granted to two named rules of another package:
// an exemption is only sound while it names every site that claims it.

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// r17Edge is one shipped rule of this module that draws a line, the border role
// it draws it in, and the ground it is drawn on — its own fill if it declares
// one, else the page's.
type r17Edge struct {
	rule, border, ground string
}

// TestSiteEdgesHoldTheGraphicFloorOnGroundsTheGateNames measures the two rules of
// this module's class lists that draw a line, over the shipped palette and a
// generated corpus.
func TestSiteEdgesHoldTheGraphicFloorOnGroundsTheGateNames(t *testing.T) {
	source, err := os.ReadFile("style.go")
	if err != nil {
		t.Fatalf("read this module's shipped rules: %v", err)
	}
	edges := r17PaintedEdges(string(source))
	if len(edges) < 2 {
		t.Fatalf("the sweep found %d rules painting an edge in style.go; the reader is broken, not the tree", len(edges))
	}
	gated := map[string]bool{}
	for _, pair := range design.EdgeRolePairs() {
		gated[pair.Foreground+" on "+pair.Background] = true
	}

	palettes := r17Corpus(t)
	measured := 0
	for _, edge := range edges {
		if !strings.HasPrefix(edge.border, "border-") {
			t.Errorf("%s draws a line in %q, which is not a border role of the role layer; the gate names roles, not literals", edge.rule, edge.border)
			continue
		}
		if edge.border == "border-secondary" {
			t.Errorf("%s draws its edge in border-secondary, the layer's decorative hairline. The only rules exempted from the graphic floor under that name are named in ui/components/review_round16_edge_ground_coverage_test.go, and this module is not among them: draw the boundary in a role the gate holds, or do not exempt it from a distance", edge.rule)
			continue
		}
		key := edge.border + " on " + edge.ground
		if !gated[key] {
			t.Errorf("%s paints its edge in %s on %s, and no pair of design.EdgeRolePairs names that ground, so a client may be issued a palette where the line disappears and no door refuses it. Name the pair in design.EdgeRolePairs (then the generator and the client door both refuse the bad palette) or do not draw the line on that ground",
				edge.rule, edge.border, edge.ground)
		}
		for _, worn := range palettes {
			for _, theme := range worn.pair.Both() {
				values, resolveErr := design.ResolveColors(theme.Tokens(), design.RoleLayer())
				if resolveErr != nil {
					t.Fatalf("%s/%s: the role layer does not resolve: %v", worn.name, theme.Name, resolveErr)
				}
				colour, okBorder := values["--pk-role-"+edge.border]
				if !okBorder {
					t.Fatalf("%s/%s: the layer declares no --pk-role-%s that %s could be drawn with", worn.name, theme.Name, edge.border, edge.rule)
				}
				surface, okGround := values["--pk-role-"+edge.ground]
				if !okGround {
					t.Fatalf("%s/%s: the layer declares no --pk-role-%s", worn.name, theme.Name, edge.ground)
				}
				if colour == surface {
					continue // the border resolves to its own ground: it paints no edge to measure
				}
				measured++
				if got := design.Contrast(colour, surface); got < design.MinContrastGraphic {
					t.Errorf("%s/%s: %s draws its edge in %s on %s and it measures %.2f:1, under the %.1f:1 SC 1.4.11 asks of the line that identifies a component",
						worn.name, theme.Name, edge.rule, edge.border, edge.ground, got, design.MinContrastGraphic)
				}
			}
		}
	}
	t.Logf("measured %d edge readings over %d palettes for rules %v", measured, len(palettes), edges)
}

// r17PaintedEdges reads, for each rule declaration of this module, the border it
// draws and the ground it stands on: its own Bg if it declares one, else the
// page's Bg — the ground the box is drawn on when it brings no fill of its own.
func r17PaintedEdges(source string) []r17Edge {
	starts := regexp.MustCompile(`(?m)^\t(cl[A-Za-z0-9_]+)\s*=`).FindAllStringSubmatchIndex(source, -1)
	page := ""
	pageGround := regexp.MustCompile(`(?m)^\tclPage\s*=.*$`).FindString(source)
	if fill := regexp.MustCompile(`Bg\(style\.Surface([A-Za-z0-9]+)\)`).FindStringSubmatch(pageGround); fill != nil {
		page = "surface-" + r17Kebab(fill[1])
	}
	var out []r17Edge
	for i, start := range starts {
		name := source[start[2]:start[3]]
		end := len(source)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rule := source[start[0]:end]
		borders := regexp.MustCompile(`BorderColor\(style\.Border([A-Za-z0-9]+)\)`).FindAllStringSubmatch(rule, -1)
		if len(borders) == 0 {
			continue
		}
		ground := ""
		if fill := regexp.MustCompile(`Bg\(style\.Surface([A-Za-z0-9]+)\)`).FindStringSubmatch(rule); fill != nil {
			ground = "surface-" + r17Kebab(fill[1])
		}
		if ground == "" {
			ground = page
		}
		for _, border := range borders {
			out = append(out, r17Edge{
				rule:   name,
				border: "border-" + r17Kebab(border[1]),
				ground: ground,
			})
		}
	}
	return out
}

// r17Kebab spells a Go constant's suffix the way the role name is written.
func r17Kebab(value string) string {
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

// r17Palette is one identity and who wears it.
type r17Palette struct {
	name string
	pair design.Pair
}

// r17Corpus is the shipped identity and a deterministic set of generated ones,
// every third carrying the brand colour a client is allowed to name.
func r17Corpus(t *testing.T) []r17Palette {
	t.Helper()
	out := []r17Palette{{name: "shipped", pair: design.Default()}}
	for i := range 100 {
		h := fnv.New64a()
		fmt.Fprintf(h, "identity-%d", i)
		seed := design.Seed{Sector: fmt.Sprintf("sector-%d", h.Sum64()%31), Name: fmt.Sprintf("Identity %d", i)}
		if i%3 == 0 {
			seed.Brand = fmt.Sprintf("#%02x%02x%02x", (i*37)%256, (i*71)%256, (i*113)%256)
		}
		pair, err := design.FromSeed(seed)
		if err != nil {
			continue // a refused seed issues no palette, so it reaches no screen
		}
		out = append(out, r17Palette{name: seed.Name, pair: pair})
	}
	if len(out) < 50 {
		t.Fatalf("the generator refused all but %d of the 100 seeds, so the corpus measures almost nothing", len(out))
	}
	return out
}
