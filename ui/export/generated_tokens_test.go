package export_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
)

// A client's generated identity exports as the same DTCG document the shipped
// palette does: the generator fills the theme struct, and every owner above it —
// the roles, the scales, the export below — reads the tokens it always read.
// This is what platformkit-mobile consumes without a second generator.
func TestExportTokensProjectsAGeneratedPair(t *testing.T) {
	t.Parallel()
	pair, err := design.FromSeed(design.Seed{Sector: "shelter", Name: "Pets", Brand: "#f0b978"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := export.ExportTokens(pair, "light", "dark")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(tokens.Modes) != 2 {
		t.Fatalf("exported %d modes, want light and dark", len(tokens.Modes))
	}
	for i, mode := range tokens.Modes {
		want := pair.Both()[i]
		if mode.Mode != want.Name {
			t.Fatalf("mode %d is %q, want %q", i, mode.Mode, want.Name)
		}
		if len(mode.Colors) != 22 {
			t.Fatalf("%s exported %d colours, want the 22 a theme owns", mode.Mode, len(mode.Colors))
		}
		for j, color := range mode.Colors {
			themeToken := want.Tokens()[j]
			if color.Name != themeToken.Name || color.Value != themeToken.Value {
				t.Errorf("%s token %d: %v, want %v", mode.Mode, j, color, themeToken)
			}
			if !strings.HasPrefix(color.Value, "#") {
				t.Errorf("%s exported %s as %q, want a literal", mode.Mode, color.Name, color.Value)
			}
		}
		if len(mode.Fonts) == 0 {
			t.Errorf("%s exported no font families", mode.Mode)
		}
	}
}
