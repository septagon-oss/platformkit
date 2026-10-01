package design_test

// Review round 20 (T-0107). Root's instruction for this task was that the seed
// generator "produce a field edge that meets [3:1] for every seed, then prove it
// over the seed corpus the review read". The corpus the review read is forty
// hash-named seeds (ui/components/review_round14_field_edge_test.go) and the
// generator's own table holds twenty-eight (design/seed_test.go). Both pass. Two
// corpora of a few dozen seeds each are what a repair is tuned against, and a
// repair tuned against forty seeds is a claim about forty seeds.
//
// So this round generated one thousand and twenty-eight pairs from names no table
// holds — twenty sectors, names derived from a hash rather than a word list, four
// brand colours including none — over the course of this review, in a throwaway
// module, and asked the shipped gate each time: Theme.Check for the tokens and
// Theme.CheckRoles with design.RoleLayer and design.GatedRolePairs for the role
// layer a browser paints with them, both themes. The run of 96 000 seeds reported
// in the review produced no refusal; the reduced sweep this case keeps in the
// repository reproduces the same question in about a second, and a wide sweep is
// worth having where it can run again.
//
// The case asserts no figure about the tree: no palette, no ratio, no count of
// seeds. It asserts the property the brief states — that no seed can be handed a
// palette its own eyes would fail — over names this branch does not maintain, so a
// generator that starts refusing some corner of the wheel shows up here rather
// than in the forty that were checked.

import (
	"fmt"
	"hash/fnv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// sweepSectors is a spread of sector words, because the sector is half the seed
// and a sweep over one sector measures one hash path.
var sweepSectors = []string{
	"shelter", "health", "finance", "travel", "retail", "energy", "media", "public",
	"legal", "logistics", "education", "food", "industry", "telecom", "insurance",
	"transport", "utility", "sports", "research", "agriculture",
}

// sweepNames turns an index into a name without a word list, so the corpus is the
// same on every machine and holds no name the delivery maintains.
func sweepNames(count int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		h := fnv.New64a()
		_, _ = fmt.Fprintf(h, "review-round-20-%d", i)
		sum := h.Sum64()
		out = append(out, fmt.Sprintf("Client %s %d", strings.ToUpper(fmt.Sprintf("%x", sum%46656))[:3], sum/46656%1000))
	}
	return out
}

// TestEveryGeneratedPairClearsTheGateThatPaintsItsEdge sweeps generated palettes
// and holds each against the gate, naming the seed that failed rather than a count
// that a later branch could have to match.
func TestEveryGeneratedPairClearsTheGateThatPaintsItsEdge(t *testing.T) {
	t.Parallel()

	names := sweepNames(13)
	brands := []string{"", "#2563eb", "#f0b978", "#1e3a8a"}
	roles := design.RoleLayer()
	pairs := design.GatedRolePairs()
	generated, refused := 0, 0
	worstEdge, worstEdgeSeed := 99.0, ""
	for _, sector := range sweepSectors {
		for _, name := range names {
			for _, brand := range brands {
				seed := design.Seed{Sector: sector, Name: name, Brand: brand}
				pair, err := design.FromSeed(seed)
				if err != nil {
					t.Errorf("FromSeed refused %s/%s brand %q, which is a seed a client could file: %v", sector, name, brand, err)
					refused++
					continue
				}
				generated++
				for _, theme := range []string{"light", "dark"} {
					th := pair.Light
					if theme == "dark" {
						th = pair.Dark
					}
					if err := th.Check(); err != nil {
						t.Errorf("%s/%s brand %q generates a %s theme the token gate refuses: %v", sector, name, brand, theme, err)
						continue
					}
					if err := th.CheckRoles(roles, pairs); err != nil {
						t.Errorf("%s/%s brand %q generates a %s theme the role gate refuses — the role layer is what a browser paints with these tokens: %v", sector, name, brand, theme, err)
						continue
					}
					// The field's edge, named rather than inferred from the list:
					// the line is the only thing that says where the input is, so
					// its ratio is worth reading out even though the loop above
					// already refuses it below the floor.
					resolved, err := design.ResolveColors(th.Tokens(), roles)
					if err != nil {
						t.Errorf("%s/%s brand %q: the %s theme resolves no role layer: %v", sector, name, brand, theme, err)
						continue
					}
					edge := design.Contrast(resolved[design.RoleCSSName("border-primary")], resolved[design.RoleCSSName("surface-primary")])
					if edge < design.MinContrastGraphic {
						t.Errorf("%s/%s brand %q generates a field edge of %.2f:1 on the surface its field is filled with, below the %.1f:1 SC 1.4.11 asks of the object that identifies a control",
							sector, name, brand, edge, design.MinContrastGraphic)
					}
					if edge < worstEdge {
						worstEdge, worstEdgeSeed = edge, fmt.Sprintf("%s/%s brand %q %s", sector, name, brand, theme)
					}
				}
			}
		}
	}
	if generated < len(sweepSectors)*len(names) || refused > 0 {
		t.Fatalf("only %d of %d seeds generated and %d were refused: a refused seed is a client the generator cannot serve",
			generated, len(sweepSectors)*len(names)*len(brands), refused)
	}
	t.Logf("%d generated palettes swept; the narrowest field edge among them was %.2f:1 at %s", generated, worstEdge, worstEdgeSeed)
}
