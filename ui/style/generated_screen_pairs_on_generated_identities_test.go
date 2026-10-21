package style_test

import (
	"sort"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// TestGeneratedScreenTextPairsClearTheFloorOnEveryGeneratedIdentity holds the
// text pairs a generated resource screen paints — the pairs
// TestGeneratedScreenTextPairsMeetTheContrastFloor measures on the shipped
// palette — to 4.5:1 on every identity the seed generator issues, plain and
// branded, in light and dark. A client wearing a generated identity sees the
// same generated screens, so a pair that screen paints and the generator's gate
// leaves out would fail the floor on that client alone. The ratio is read
// through ResolveColors and this package's own WCAG arithmetic, not through
// Theme.CheckRoles, so the generator's gate and this reading are independent.
func TestGeneratedScreenTextPairsClearTheFloorOnEveryGeneratedIdentity(t *testing.T) {
	pairs := []struct{ fg, bg style.Color }{
		{style.FgPrimary, style.SurfacePrimary}, {style.FgPrimary, style.SurfaceSecondary},
		{style.FgSecondary, style.SurfacePrimary}, {style.FgSecondary, style.SurfaceSecondary},
		{style.FgMuted, style.SurfacePrimary}, {style.FgMuted, style.SurfaceSecondary},
		{style.FgLink, style.SurfacePrimary}, {style.FgLink, style.SurfaceSecondary},
		{style.FgBrand, style.SurfacePrimary}, {style.FgBrand, style.SurfaceSecondary},
		{style.FgOnBrand, style.SurfaceBrand}, {style.FgOnBrand, style.SurfaceDanger},
		{style.FgOnInverse, style.SurfaceInverse},
	}
	corpus := brandedCorpus(t)
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	// The corpus is 400 plain seeds and the branded seeds the generator accepts;
	// a corpus that shrank to the shipped palette would measure nothing new.
	if len(names) < 400 {
		t.Fatalf("the corpus holds %d identities, want at least 400 generated ones", len(names))
	}
	measured := 0
	for _, name := range names {
		for _, theme := range corpus[name].Both() {
			got, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
			if err != nil {
				t.Fatalf("%s/%s: %v", name, theme.Name, err)
			}
			for _, p := range pairs {
				fg, bg := got["--pk-role-"+string(p.fg)], got["--pk-role-"+string(p.bg)]
				if ratio := contrast(fg, bg); ratio < design.MinContrast {
					t.Errorf("%s/%s: %s on %s is %.2f:1, the floor is %.1f:1", name, theme.Name, p.fg, p.bg, ratio, design.MinContrast)
				}
				measured++
			}
		}
	}
	t.Logf("%d readings over %d identities", measured, len(names))
}
