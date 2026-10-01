package components_test

// Review round 21 (T-0107).
//
// Rounds 14, 15 and 16 each found a ground a component's edge is drawn on that
// the gate did not name, and the third of them wrote a coverage case. That case
// cannot find the next one: when a rule declares no fill of its own it measures
// the rule on `r16Grounds()`, which it reads out of `design.EdgeRolePairs()`
// itself (ui/components/review_round16_edge_ground_coverage_test.go:66-79 — "the
// grounds a rule that declares no fill of its own is drawn on. They are read from
// design.EdgeRolePairs"). A fifth ground is invisible to it by construction: the
// list it measures against is the list it is testing.
//
// The fifth ground exists and is painted. One rule in this package draws a line
// with `border-primary`, declares no fill, and is applied inside a container that
// declares a fill the four gated grounds never name:
//
//	ui/components/classlists.go:689-691 clSidebarSectionHeader  BorderBottom(Border1).BorderColor(style.BorderPrimary)  — no Bg
//	ui/components/classlists.go:654     clSidebarColumnAdmin    Bg(style.SurfaceInverse)                                — the ground
//	ui/components/sidebar.go:202        headerClass := clSidebarSectionHeaderAdmin  (every sidebar that is not the
//	                                                              content flavour and carries a labelled section)
//
// The reference installation serves it: apps/platformkit/modules.go:250 mounts the
// storybook, whose pk-ui.component.sidebar/default example
// (ui/components/examples/gallery.go:247) sets no Flavor — so flavor = "admin" —
// and one section labelled "Operate".
//
// `surface-inverse` is `sidebar-bg`, a token a seed decides (design.identityTokens)
// and a client may override (design.Client.Tokens). Measured through the role
// layer the sheet paints, over the corpus this package's own cases use:
//
//	shipped (design.Default)               border-primary on surface-inverse = 3.77:1
//	400 generated pairs, both themes       35 of 800 readings under 3:1, worst 2.65:1
//	client design.yaml overrides accepted  an accepted file at 2.05:1 (sidebar-bg #5f3257, light)
//
// So an identity the generator issues, or a client file `Client.Resolve` accepts,
// can carry a sidebar whose section lines have no edge against the panel they are
// drawn on, and nothing refuses it. That is the same ground round 11 measured,
// recorded in the changelog and pinned for the *ring* alone
// (design/ring_ground_test.go:TestShippedRingHoldsOnTheSidebarGroundTheGateDoesNotName);
// the line on that ground has never been measured by anything.
//
// Three cases, in the order a reader should check them:
//
//  1. TestTheShippedEdgeHoldsOnTheSidebarGroundTheEdgeGateDoesNotName — the pin
//     the deferral deserves and only the ring has: the shipped palette's own
//     reading, both themes, at MinContrastGraphic. It passes at these bytes and
//     fails the day a token change erases the line, which is the failure mode
//     round 14 filed.
//  2. TestTheLineTheSidebarPaintsIsGatedOnTheGroundTheSidebarPaints — the defect.
//     It fails now: `border-primary` on `surface-inverse` is in no gated list. It
//     has the same way to pass its predecessors had — name the pair in
//     design.EdgeRolePairs (the door then refuses a client file and a seed whose
//     line vanishes) or stop drawing the line there.
//  3. TestTheSidebarStillDrawsThatLineOnThatGround — the witness, in round 3's
//     shape: it fails if the premise itself goes away, so cases 1 and 2 cannot be
//     made green by the paint quietly leaving the tree.

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// r21Ground is the ground the sidebar's own column declares under its section
// headers, and r21Edge the role the headers draw their line with.
const (
	r21Edge   = "border-primary"
	r21Ground = "surface-inverse"
)

// r21Corpus is the shipped palette plus a generated family, in the shape every
// other edge case here uses.
func r21Corpus(t *testing.T) []design.Pair {
	t.Helper()
	pairs := []design.Pair{design.Default()}
	sectors := []string{"education", "health", "finance", "retail", "logistics", "energy", "public", "media"}
	for i := 0; i < 400; i++ {
		seed := design.Seed{Sector: sectors[i%len(sectors)], Name: "Client " + strconv.Itoa(i)}
		pair, err := design.FromSeed(seed)
		if err != nil {
			continue
		}
		pairs = append(pairs, pair)
	}
	return pairs
}

// r21Resolve reads the pair the way a stylesheet paints it: the role layer
// resolved over one theme's tokens.
func r21Resolve(t *testing.T, theme design.Theme) map[string]design.SRGBA {
	t.Helper()
	values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
	if err != nil {
		t.Fatalf("%s: the role layer does not resolve: %v", theme.Name, err)
	}
	return values
}

func r21Gated(edge, ground string) (design.RolePair, bool) {
	for _, pair := range design.GatedRolePairs() {
		if design.RoleCSSName(pair.Foreground) == design.RoleCSSName(edge) &&
			design.RoleCSSName(pair.Background) == design.RoleCSSName(ground) {
			return pair, true
		}
	}
	return design.RolePair{}, false
}

// TestTheShippedEdgeHoldsOnTheSidebarGroundTheEdgeGateDoesNotName pins the one
// reading of this pair that no case in the tree takes: the installation's own
// palette, both modes, at the graphic floor. The ring has exactly this pin; the
// line drawn on the same ground by the same component has none.
func TestTheShippedEdgeHoldsOnTheSidebarGroundTheEdgeGateDoesNotName(t *testing.T) {
	for _, theme := range design.Default().Both() {
		values := r21Resolve(t, theme)
		got := design.Contrast(values[design.RoleCSSName(r21Edge)], values[design.RoleCSSName(r21Ground)])
		if got < design.MinContrastGraphic {
			t.Errorf("%s: the sidebar's section line measures %.3f:1 on the panel it is drawn on, below the %.1f:1 SC 1.4.11 asks of the visual information that identifies a component",
				theme.Name, got, design.MinContrastGraphic)
		}
	}
}

// TestTheLineTheSidebarPaintsIsGatedOnTheGroundTheSidebarPaints is the finding:
// the pair is painted by a shipped rule and named by no gate, so neither
// design.FromSeed nor design.Client.Resolve can refuse an identity that erases it.
func TestTheLineTheSidebarPaintsIsGatedOnTheGroundTheSidebarPaints(t *testing.T) {
	pair, gated := r21Gated(r21Edge, r21Ground)
	if !gated {
		t.Errorf("%s on %s is drawn by clSidebarSectionHeader (ui/components/classlists.go:689-691) inside clSidebarColumnAdmin's Bg(style.SurfaceInverse) (classlists.go:654), applied by ui/components/sidebar.go:202 and served at apps/platformkit/modules.go:250, and appears in no gated list (design.EdgeRolePairs names surface-primary, -secondary, -tertiary and -hover; the token gate names surface-primary, -canvas and -muted): name the pair at MinContrastGraphic or stop drawing the line there", r21Edge, r21Ground)
		return
	}
	// Once the pair is named, the floor has to hold over the palettes a client can
	// actually be issued, which is what naming it buys.
	corpus := r21Corpus(t)
	worst, worstName := 99.0, ""
	for _, worn := range corpus {
		for _, theme := range worn.Both() {
			values := r21Resolve(t, theme)
			if got := design.Contrast(values[design.RoleCSSName(r21Edge)], values[design.RoleCSSName(r21Ground)]); got < worst {
				worst, worstName = got, theme.Name
			}
		}
	}
	t.Logf("corpus %d palettes, narrowest %s reading %.4f (%s)", len(corpus), r21Ground, worst, worstName)
	if worst < pair.Min {
		t.Errorf("%s on %s is gated at %.1f:1 and the narrowest reading over %d palettes is %.3f:1 (%s)",
			r21Edge, r21Ground, pair.Min, len(corpus), worst, worstName)
	}
}

// TestTheSidebarStillDrawsThatLineOnThatGround refuses the premise rather than
// letting the measurement quietly stop meaning anything. It reads the shipped
// sources: the rule that draws the line, the container that declares the ground,
// and the component that puts the two together.
func TestTheSidebarStillDrawsThatLineOnThatGround(t *testing.T) {
	rules, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatalf("read the shipped rules: %v", err)
	}
	source := string(rules)
	header, found := r21Declaration(source, "clSidebarSectionHeader =")
	if !found {
		t.Fatal("clSidebarSectionHeader is gone from classlists.go: the pair this file measures no longer exists, and the file should be retargeted, not left green")
	}
	if !strings.Contains(header, "BorderColor(style.BorderPrimary)") {
		t.Errorf("clSidebarSectionHeader no longer draws its line with border-primary (%s): this file's premise moved", header)
	}
	if strings.Contains(header, "Bg(style.") {
		t.Errorf("clSidebarSectionHeader now declares its own fill (%s): the ground is no longer inherited, so the pair is a declared one", header)
	}
	column, found := r21Declaration(source, "clSidebarColumnAdmin =")
	if !found {
		t.Fatal("clSidebarColumnAdmin is gone from classlists.go")
	}
	if !strings.Contains(column, "Bg(style.SurfaceInverse)") {
		t.Errorf("clSidebarColumnAdmin no longer fills itself with surface-inverse (%s): the ground this file measures moved", column)
	}
	applied, err := os.ReadFile("sidebar.go")
	if err != nil {
		t.Fatalf("read sidebar.go: %v", err)
	}
	if !strings.Contains(string(applied), "clSidebarSectionHeaderAdmin") {
		t.Error("sidebar.go no longer applies clSidebarSectionHeaderAdmin, so the admin section header is not painted by any shipped page")
	}
}

// r21Declaration returns the text of one declaration inside a var block: from its
// marker to the next line written at the block's own indent, which is the next
// declaration. Continuation lines of a builder chain are indented deeper.
func r21Declaration(source, marker string) (string, bool) {
	start := strings.Index(source, marker)
	if start < 0 {
		return "", false
	}
	lines := strings.Split(source[start:], "\n")
	var out []string
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "\t\t") {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), true
}
