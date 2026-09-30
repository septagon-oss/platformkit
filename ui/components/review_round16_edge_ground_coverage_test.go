package components_test

// Review round 16 (T-0107). Rounds 14 and 15 each found a ground this package
// paints a component's edge on that the one gate did not name: first the card,
// the canvas and the muted panel, then the fill a hovered control moves under its
// own line. Both are gated now — design.EdgeRolePairs names border-primary on
// surface-primary, -secondary, -tertiary and -hover at MinContrastGraphic, and
// FromSeed refuses a palette whose role layer fails that list — so the question
// left is whether the rule stayed a rule or became four anecdotes.
//
// Nothing in the repository reads the list back against the code that paints it.
// design/roles.go says the grounds were "read out of classlists.go rather than
// guessed"; that is a claim about a grep nobody runs again, and the next state
// (a disabled field, a selected row, a dialog footer) is exactly where rounds 14
// and 15 landed. This case reads the shipped rules themselves, and asks of every
// edge they draw, in every palette a client can be issued, the ratio SC 1.4.11
// asks of the visual information that identifies a component.
//
// Two exemptions are stated rather than assumed:
//
//   - a border that resolves to the colour of the fill it sits on carries nothing;
//     the fill is what shows the extent (clCheckboxIndicatorActive draws
//     border-brand on a surface-brand box). The case measures the pair's two
//     colours rather than trusting the names;
//   - border-secondary is the layer's documented decorative hairline. The case
//     does not exempt it silently: it lists the rules allowed to draw an edge with
//     it, so a new component cannot borrow the exemption the way a field nearly did.
//
// The case has a way to pass and it is the same one its two predecessors had:
// name the pair in design.EdgeRolePairs (then the door refuses a client whose
// palette puts that line under the floor and the generator refuses the seed), or
// stop drawing the line there. While neither is done it fails, naming the rule,
// the border role, the ground, the palette and the ratio it measured.

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// r16Edge is one shipped rule that draws a line, the role it draws it in, and the
// grounds it declares underneath itself.
type r16Edge struct {
	rule, border string
	grounds      []string
}

// r16Palette is one identity and who wears it.
type r16Palette struct {
	name string
	pair design.Pair
}

// r16Sources are this package's shipped rule declarations.
var r16Sources = []string{"classlists.go", "sidebar_disclosure.go"}

// r16Grounds are the surfaces a card can be raised onto and the fill a hovered
// control mixes under itself — the grounds a rule that declares no fill of its
// own is drawn on. They are read from design.EdgeRolePairs, so this case measures
// what the gate claims rather than a list written beside it.
func r16Grounds() []string {
	seen := map[string]bool{}
	var grounds []string
	for _, pair := range design.EdgeRolePairs() {
		if !seen[pair.Background] {
			seen[pair.Background] = true
			grounds = append(grounds, pair.Background)
		}
	}
	sort.Strings(grounds)
	return grounds
}

// r16Decorative names the only rules allowed to draw a component's edge with the
// layer's decorative hairline. A rule outside this list that paints its boundary
// in border-secondary is the finding this pin exists to catch.
var r16Decorative = map[string]bool{"clSpinner": true, "clTabsUnderlineIdle": true}

// TestEveryEdgeThisPackagePaintsHoldsTheFloorOnTheGroundItIsPaintedOn measures
// each edge the shipped rules draw, on each ground they or their container give
// them, over the shipped palette and a generated corpus.
func TestEveryEdgeThisPackagePaintsHoldsTheFloorOnTheGroundItIsPaintedOn(t *testing.T) {
	var edges []r16Edge
	for _, source := range r16Sources {
		text, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read the shipped rules: %v", err)
		}
		edges = append(edges, r16PaintedEdges(string(text))...)
	}
	if len(edges) < 10 {
		t.Fatalf("the sweep found %d rules painting an edge in %v; the reader is broken, not the tree", len(edges), r16Sources)
	}
	grounds := r16Grounds()
	if len(grounds) == 0 {
		t.Fatal("design.EdgeRolePairs names no ground, so there is nothing to measure a field's edge on")
	}

	decorative := map[string]bool{}
	measured := map[string]bool{}
	corpus := r16Corpus(t)
	for _, edge := range edges {
		targets := edge.grounds
		if len(targets) == 0 {
			targets = grounds
		}
		for _, ground := range targets {
			for _, worn := range corpus {
				for _, theme := range worn.pair.Both() {
					values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
					if err != nil {
						t.Fatalf("%s/%s: the role layer does not resolve: %v", worn.name, theme.Name, err)
					}
					colour, okBorder := values["--pk-role-"+edge.border]
					if !okBorder {
						t.Fatalf("%s: the layer declares no --pk-role-%s that %s could be drawn with", theme.Name, edge.border, edge.rule)
					}
					surface, okGround := values["--pk-role-"+ground]
					if !okGround {
						t.Fatalf("%s: the layer declares no --pk-role-%s", theme.Name, ground)
					}
					if colour == surface {
						continue // the border resolves to its own ground: it paints no edge to measure
					}
					if edge.border == "border-secondary" {
						decorative[edge.rule] = true
						continue
					}
					measured[edge.border+"|"+ground] = true
					if got := design.Contrast(colour, surface); got < design.MinContrastGraphic {
						t.Errorf("%s/%s: %s draws its edge in %s on %s and it measures %.2f:1, under the %.1f:1 SC 1.4.11 asks of the line that identifies a component. No other door in the repository measures a pair this list does not name, so naming the pair in design.EdgeRolePairs or not drawing the line there are the two ways this clears",
							worn.name, theme.Name, edge.rule, edge.border, ground, got, design.MinContrastGraphic)
					}
				}
			}
		}
	}
	for rule := range r16Decorative {
		if !decorative[rule] {
			t.Errorf("%s no longer draws an edge in border-secondary, so the exemption named in this file is one wider than the tree", rule)
		}
	}
	for rule := range decorative {
		if !r16Decorative[rule] {
			t.Errorf("%s draws a component's edge in border-secondary, which measures under the graphic floor by construction; name the pair in design.EdgeRolePairs or draw the edge in a role the gate holds there", rule)
		}
	}

	// The other direction: a ground the gate names that nothing paints is a pair
	// a client is refused over for no screen at all.
	paintedGrounds := map[string]bool{}
	for key := range measured {
		paintedGrounds[strings.SplitN(key, "|", 2)[1]] = true
	}
	for _, ground := range grounds {
		if !paintedGrounds[ground] {
			t.Errorf("design.EdgeRolePairs gates border-primary on %s, but no shipped rule draws an edge on it, so the pair refuses clients and paints nothing", ground)
		}
	}
	t.Logf("measured %d border/ground pairs on %d grounds over %d palettes", len(measured), len(grounds), len(corpus))
}

// r16PaintedEdges reads, for each rule declaration, the border it draws and the
// fill it stands on — including the fill a hovered state moves under it, which is
// where round 15's ground came from.
func r16PaintedEdges(source string) []r16Edge {
	starts := regexp.MustCompile(`(?m)^\t+(cl[A-Za-z0-9_]+)\s*=|(?m)^\t+"([a-z0-9_-]+)":\s*style\.New\(\)`).FindAllStringSubmatchIndex(source, -1)
	var out []r16Edge
	for i, start := range starts {
		name := "[entry]"
		if start[2] >= 0 {
			name = source[start[2]:start[3]]
		} else if start[4] >= 0 {
			name = "[" + source[start[4]:start[5]] + "]"
		}
		end := len(source)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rule := source[start[0]:end]
		borders := regexp.MustCompile(`BorderColor\(style\.Border([A-Za-z0-9]+)\)`).FindAllStringSubmatch(rule, -1)
		if len(borders) == 0 {
			continue
		}
		entry := r16Edge{rule: name}
		for _, border := range borders {
			entry.border = "border-" + r16Kebab(border[1])
		}
		fills := regexp.MustCompile(`(?:Bg|hoverBg)\(style\.Surface([A-Za-z0-9]+)\)`).FindAllStringSubmatch(rule, -1)
		seen := map[string]bool{}
		for _, fill := range fills {
			ground := "surface-" + r16Kebab(fill[1])
			if !seen[ground] {
				seen[ground] = true
				entry.grounds = append(entry.grounds, ground)
			}
		}
		out = append(out, entry)
	}
	return out
}

// r16Kebab spells a Go constant's suffix the way the role name is written.
func r16Kebab(value string) string {
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

// r16Corpus is the shipped identity and a deterministic set of generated ones,
// every third carrying the brand colour a client is allowed to name.
func r16Corpus(t *testing.T) []r16Palette {
	t.Helper()
	out := []r16Palette{{"shipped", design.Default()}}
	for i := range 200 {
		h := fnv.New64a()
		fmt.Fprintf(h, "identity-%d", i)
		seed := design.Seed{Sector: fmt.Sprintf("sector-%d", h.Sum64()%31), Name: fmt.Sprintf("Identity %d", i)}
		if i%3 == 0 {
			seed.Brand = fmt.Sprintf("#%02x%02x%02x", (i*37)%256, (i*71)%256, (i*113)%256)
		}
		pair, err := design.FromSeed(seed)
		if err != nil {
			// A seed the generator refuses issues no palette, so it reaches no
			// screen; the case measures what a client can actually be issued.
			continue
		}
		out = append(out, r16Palette{seed.Name, pair})
	}
	if len(out) < 100 {
		t.Fatalf("the generator refused all but %d of the 200 seeds, so the corpus measures almost nothing", len(out))
	}
	return out
}
