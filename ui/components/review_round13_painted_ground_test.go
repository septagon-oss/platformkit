package components_test

import (
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// Review round 13.
//
// design.GatedRolePairs lists the pairs the gate certifies. This case lists the
// pairs ui/components actually paints that are NOT in that list, read from the
// classlist source (each carries its file:line), and measures them at the body
// floor anyway. The gate list is the promise a client's file is held to; this is
// the promise the shipped components make, and nothing else in the tree reads
// these pairs. The moment the generator drifts a hover ground or a soft tint,
// this is the case that says so instead of a person's screen.
//
// Every pair here is one a classlist composes in a single declaration, so the
// foreground really does sit on the ground:
//
//	ui/components/classlists.go:339 clModalClose            fg-muted    on surface-hover
//	ui/components/classlists.go:110 clButtonVariant         fg-primary  on surface-hover
//	ui/components/classlists.go:346 clModalCancel           fg-secondary on surface-hover
//	ui/components/classlists.go:107 clButtonVariant         fg-on-brand on surface-brand-hover
//
// surface-overlay is deliberately absent: it resolves translucent, so a ratio
// across it is this repository's to composite rather than to measure, and a case
// that pretended otherwise would assert nothing.
var paintedUngatedPairs = [][2]string{
	{"fg-muted", "surface-hover"},
	{"fg-primary", "surface-hover"},
	{"fg-secondary", "surface-hover"},
	{"fg-on-brand", "surface-brand-hover"},
}

// The pairs the gate does name, restated here as the components paint them, so a
// pair removed from the gate's list while a component keeps painting it cannot
// pass this file silently.
var paintedGatedPairs = [][2]string{
	{"fg-brand", "surface-brand-soft"},
	{"fg-success", "surface-success-soft"},
	{"fg-warning", "surface-warning-soft"},
	{"fg-danger", "surface-danger-soft"},
	{"fg-info", "surface-info-soft"},
	{"fg-on-brand", "surface-brand"},
	{"fg-on-brand", "surface-danger"},
	{"fg-secondary", "surface-tertiary"},
	{"fg-placeholder", "surface-primary"},
	{"fg-primary", "surface-primary"},
	{"fg-secondary", "surface-primary"},
	{"fg-secondary", "surface-secondary"},
}

func TestPaintedPairsHoldTheBodyFloorOnGeneratedPalettes(t *testing.T) {
	seeds := []string{"finance", "health", "education", "logistics", "energy", "retail", "legal", "media"}
	gated := map[string]bool{}
	for _, p := range design.GatedRolePairs() {
		gated[p.Foreground+"|"+p.Background] = true
	}
	for _, p := range paintedGatedPairs {
		if !gated[p[0]+"|"+p[1]] {
			t.Errorf("%s on %s is painted by a classlist and named by no gate list any more", p[0], p[1])
		}
	}
	for _, p := range paintedUngatedPairs {
		if gated[p[0]+"|"+p[1]] {
			t.Errorf("%s on %s is now gated: this case should read it as part of the gate and stop counting it as the guard below the gate", p[0], p[1])
		}
	}

	pairs := append(append([][2]string{}, paintedUngatedPairs...), paintedGatedPairs...)
	worst := func(pair [2]string) (float64, string, int) {
		lowest, at, measured := 100.0, "", 0
		check := func(label string, pair2 design.Pair) {
			for _, mode := range []string{"light", "dark"} {
				theme := pair2.Light
				if mode == "dark" {
					theme = pair2.Dark
				}
				values, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
				if err != nil {
					t.Fatalf("resolve the role layer of %s: %v", label, err)
				}
				fg, bg := values["--pk-role-"+pair[0]], values["--pk-role-"+pair[1]]
				if len(fg) != 4 || len(bg) != 4 || fg[3] != 1 || bg[3] != 1 {
					t.Fatalf("%s or %s does not resolve to an opaque colour at %s", pair[0], pair[1], label)
				}
				ratio := design.Contrast(fg, bg)
				measured++
				if ratio < lowest {
					lowest, at = ratio, label+" "+mode
				}
			}
		}
		check("shipped", design.Default())
		for i := 0; i < 120; i++ {
			seed := design.Seed{Sector: seeds[i%len(seeds)], Name: "Client" + strconv.Itoa(i)}
			pair2, err := design.FromSeed(seed)
			if err != nil {
				t.Fatalf("seed %q generates no pair: %v", seed.Name, err)
			}
			check(seed.Name, pair2)
		}
		return lowest, at, measured
	}

	for _, pair := range pairs {
		lowest, at, measured := worst(pair)
		if measured < 200 {
			t.Fatalf("pair %s on %s measured only %d readings: this case measured nothing", pair[0], pair[1], measured)
		}
		if lowest < design.MinContrast {
			t.Errorf("%s on %s measures %.4f at %s, below the body floor %.1f: a classlist paints it and the gate names it: %v",
				pair[0], pair[1], lowest, at, design.MinContrast, gated[pair[0]+"|"+pair[1]])
		}
		t.Logf("%-14s on %-20s closest %6.4f at %s over %d readings", pair[0], pair[1], lowest, at, measured)
	}
}
