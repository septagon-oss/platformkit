package components_test

// Review round 14 (T-0107). design/contrast.go introduces the ratio this
// repository measures and the two floors it applies — 4.5:1 for body copy
// (SC 1.4.3) and 3.0:1 for "a graphical object such as a focus ring"
// (SC 1.4.11) — and its own comment then states the scope: "Borders and tints
// carry no information and are not listed."
//
// A text field's border is the counter-example, and this package paints it.
// clInput fills the field with the same role as the card it stands on
// (Bg(style.SurfacePrimary), the ground of clCardFrame), so the fill says
// nothing about where the field is; the 1px solid line in
// BorderColor(style.BorderPrimary) is the only visual information that
// identifies the component and its extent, which is exactly what SC 1.4.11
// asks to reach 3:1 — the standard's own worked example is a text input's
// border. The focus ring, which carries less, is gated on three grounds; the
// line that says "type here" is gated on none.
//
// The case reads the pair out of this package's declarations rather than
// asserting a colour, so it has a way to pass: give the field a ground of its
// own (then the fill carries its extent and the pair is not painted) or bring
// the line up to the floor the gate already holds a ring to (then the
// measurement clears). While neither is done it fails, naming the role, the
// ground, the measured ratio and the palette it was measured on.

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// TestFieldEdgeReachesTheGraphicFloorOnItsOwnGround measures the line a field
// draws itself with against the ground the field is drawn on.
func TestFieldEdgeReachesTheGraphicFloorOnItsOwnGround(t *testing.T) {
	source, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)

	field := classRule(t, text, "clInput")
	fieldBg, hasFieldGround := firstPaint(field, `Bg\(style\.Surface([A-Za-z0-9]+)\)`)
	if !hasFieldGround {
		t.Fatal("clInput no longer declares a ground this case can read")
	}
	borderRole, hasEdge := firstPaint(classRule(t, text, "clInputNormal"), `BorderColor\(style\.Border([A-Za-z0-9]+)\)`)
	if !hasEdge {
		t.Fatal("clInputNormal no longer declares a border colour this case can read")
	}
	cardBg, hasCardGround := firstPaint(classRule(t, text, "clCardFrame"), `Bg\(style\.Surface([A-Za-z0-9]+)\)`)
	if !hasCardGround {
		t.Fatal("clCardFrame no longer declares a ground this case can read")
	}
	if fieldBg != cardBg {
		t.Skipf("clInput now fills %s on a card painted %s, so the field's own ground carries its extent and the border carries nothing", fieldBg, cardBg)
	}

	// The grounds a field is actually shown on: the card it stands on, and the
	// page canvas behind that card. clInput's fill equals the first and is within
	// about 1.1:1 of the second, so on both the line is what identifies it.
	grounds := []string{"surface-" + kebab(fieldBg), "surface-secondary"}
	corpus := append([]namedPair{{"shipped", design.Default()}}, generatedCorpus(t, 40)...)
	worst, worstWhere, readings, under := 21.0, "", 0, 0
	for _, worn := range corpus {
		pair := worn.pair
		for _, theme := range pair.Both() {
			values, resolveErr := design.ResolveColors(theme.Tokens(), design.RoleLayer())
			if resolveErr != nil {
				t.Fatalf("%s: the role layer does not resolve: %v", theme.Name, resolveErr)
			}
			border, okBorder := values["--pk-role-border-"+kebab(borderRole)]
			if !okBorder {
				t.Fatalf("%s: the layer declares no --pk-role-border-%s for the Border%s the field is drawn with", theme.Name, kebab(borderRole), borderRole)
			}
			for _, ground := range grounds {
				surface, okSurface := values["--pk-role-"+ground]
				if !okSurface {
					t.Fatalf("%s: the layer declares no --pk-role-%s", theme.Name, ground)
				}
				if border[3] != 1 || surface[3] != 1 {
					t.Errorf("%s [%s]: border-%s on %s carries alpha, so it has no ratio until something is composited behind it", worn.name, theme.Name, kebab(borderRole), ground)
					continue
				}
				readings++
				got := design.Contrast(border, surface)
				if got < worst {
					worst, worstWhere = got, fmt.Sprintf("%s/%s: border-%s on %s", worn.name, theme.Name, kebab(borderRole), ground)
				}
				if got < design.MinContrastGraphic {
					under++
				}
			}
		}
	}
	if under > 0 {
		t.Errorf("%d of %d readings of a field's edge on the ground it is drawn on fall below the %.1f:1 SC 1.4.11 asks of the visual information that identifies a component (worst %.2f:1 at %s): %s is filled %s, the same role as the card it stands on, so that line is the only thing on the page that says where the field is",
			under, readings, design.MinContrastGraphic, worst, worstWhere, "clInput", "surface-"+kebab(fieldBg))
	}

	// The second half of the same invariant the painted-pair sweep holds: a pair
	// this package paints is a pair the one gate names, because Client.Resolve and
	// ui/export refuse over that list and a pair outside it is refused by nobody.
	gated := false
	for _, pair := range design.GatedRolePairs() {
		if design.RoleCSSName(pair.Foreground) == "--pk-role-border-"+kebab(borderRole) {
			gated = true
		}
	}
	if !gated {
		t.Errorf("no gated role pair names border-%s, so no door in the repository refuses a client whose field edge falls below the floor: design/contrast.go states %q and this package paints the border as the field's only edge",
			kebab(borderRole), "Borders and tints carry no information and are not listed")
	}
}

// namedPair is one palette and who wears it, so a failure names the client.
type namedPair struct {
	name string
	pair design.Pair
}

// classRule returns the body of one class-list declaration, from its
// style.New() to the next top-level declaration, so a rule written over four
// lines is read as the one rule it is.
func classRule(t *testing.T, source, name string) string {
	t.Helper()
	start := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*style\.New\(\)`).FindStringIndex(source)
	if start == nil {
		t.Fatalf("classlists.go declares no %s written as a style.New() chain", name)
	}
	rest := source[start[1]:]
	end := regexp.MustCompile(`(?m)^\tcl[A-Za-z0-9_]+\s*=`).FindStringIndex(rest)
	if end == nil {
		return rest
	}
	return rest[:end[0]]
}

// firstPaint reads the first style constant a chain sets under pattern and
// returns it in the role vocabulary: SurfacePrimary is style.SurfacePrimary.
func firstPaint(rule, pattern string) (string, bool) {
	found := regexp.MustCompile(pattern).FindStringSubmatch(rule)
	if found == nil {
		return "", false
	}
	return found[1], true
}

// generatedCorpus is a deterministic set of client identities: the first n of a
// hash, chosen to be wide rather than to flatter the gate.
func generatedCorpus(t *testing.T, n int) []namedPair {
	t.Helper()
	out := make([]namedPair, 0, n)
	for i := range n {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		seed := design.Seed{Sector: "sector-" + fmt.Sprint(h.Sum64()%977), Name: "identity-" + fmt.Sprint(i)}
		pair, err := design.FromSeed(seed)
		if err != nil {
			// A seed the generator refuses is a seed that ships no paint; the case
			// measures what a client can actually be issued.
			continue
		}
		out = append(out, namedPair{seed.Name, pair})
	}
	if len(out) == 0 {
		t.Fatal("the generator refused the whole corpus, so there is no palette to measure")
	}
	return out
}
