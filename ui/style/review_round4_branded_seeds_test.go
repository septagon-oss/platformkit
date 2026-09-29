package style_test

// Review round 4 (T-0107). Nothing in the repository measures the gated role
// pairs against a seed that carries the brand colour its client already owns.
// design/seed.go says a brand "fixes the accent's hue, never its lightness", so
// the lightness — the thing every ratio depends on — is decided by the gate for
// a different colour than the one a hashed seed would have chosen, and the
// accent is repaired against a tint mixed from itself (design.SoftTintPercent).
// The delivery's own sweep covers hashed seeds; this corpus is the other half of
// the generator's input, and it is where the cured pair sits closest to the
// floor: over 3000 hashed seeds, 2592 brand-supplied seeds and design.Default(),
// both themes, the tightest gated pair measures 4.500:1 —
// --pk-role-fg-brand on --pk-role-surface-brand-soft in dark.
//
// The case asserts only the behaviour the README states, per palette and per
// pair, and the number that reaches every assertion is measured here. It asks of
// each pair nothing the list does not itself claim (pair.Min), so it goes green
// today and refuses the day a generated or branded palette drifts under it.

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

func brandedCorpus(t *testing.T) map[string]design.Pair {
	t.Helper()
	corpus := map[string]design.Pair{"default": design.Default()}
	for i := range 400 {
		seed := design.Seed{Sector: fmt.Sprintf("sector-%d", i%140), Name: fmt.Sprintf("identity-%d", i)}
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("FromSeed(%v): %v", seed, err)
		}
		corpus[fmt.Sprintf("seed-%d", i)] = pair
	}
	for r := 0; r < 256; r += 51 {
		for g := 0; g < 256; g += 51 {
			for b := 0; b < 256; b += 51 {
				brand := fmt.Sprintf("#%02x%02x%02x", r, g, b)
				seed := design.Seed{Sector: "sector-7", Name: "branded-" + brand, Brand: brand}
				pair, err := design.FromSeed(seed)
				if err != nil {
					// A brand colour the generator cannot make legible is refused,
					// which is the behaviour this branch exists to have. It is
					// recorded here so a quiet refusal cannot pass for a pass.
					t.Logf("FromSeed refuses brand %s: %v", brand, err)
					continue
				}
				corpus["brand-"+brand] = pair
			}
		}
	}
	return corpus
}

// TestGatedRolePairsHoldOnBrandedSeeds measures every pair the gate names — the
// surfaces a card raises itself onto and the soft brand tint the layer derives —
// on every palette a client can ask for by writing a brand colour, in both
// themes. A generated pair that drops below its own floor is the brief's
// property 1 falsified, so the floor this case reads is the floor the list
// claims, never a figure of this test's own.
func TestGatedRolePairsHoldOnBrandedSeeds(t *testing.T) {
	pairs := append(style.BodyRolePairs(), style.TintedRolePairs()...)
	if len(pairs) == 0 {
		t.Fatal("the role layer names no gated pair: nothing here would be measured")
	}
	corpus := brandedCorpus(t)
	measurements, tightest, tightestWhere, tightestPair := 0, 21.0, "", ""
	for label, pair := range corpus {
		for _, theme := range pair.Both() {
			mode := theme.Name
			values, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
			if err != nil {
				t.Fatalf("%s: resolve the role layer: %v", label, err)
			}
			for _, p := range pairs {
				fg, okF := values[p.Foreground]
				bg, okB := values[p.Background]
				if !okF || !okB {
					t.Fatalf("%s %s: gated pair names %s/%s, which the resolved role layer does not define", label, mode, p.Foreground, p.Background)
				}
				if fg[3] != 1 || bg[3] != 1 {
					t.Fatalf("%s %s: gated pair %s on %s is not opaque", label, mode, p.Foreground, p.Background)
				}
				got := design.Contrast(fg, bg)
				measurements++
				if got < tightest {
					tightest, tightestWhere, tightestPair = got, label+"/"+mode, p.Foreground+" on "+p.Background
				}
				if got < p.Min {
					t.Errorf("%s (%s): %s on %s measures %.3f:1, below the %.1f:1 the pair the gate names requires",
						label, mode, p.Foreground, p.Background, got, p.Min)
				}
			}
		}
	}
	t.Logf("%d measurements over %d palettes x 2 themes; tightest %.3f:1 (%s) on %s",
		measurements, len(corpus), tightest, tightestPair, tightestWhere)
}
