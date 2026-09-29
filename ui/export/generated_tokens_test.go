package export_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/style"
)

// A client's generated identity exports as the same DTCG document the shipped
// palette does: the generator fills the theme struct, and every owner above it —
// the roles, the scales, the export below — reads the tokens it always read.
// This is what platformkit-mobile consumes without a second generator.
func TestExportTokensProjectsAGeneratedPair(t *testing.T) {
	t.Parallel()
	pair, err := design.Client{
		Slug: "pets",
		Seed: design.Seed{Sector: "shelter", Name: "Pets", Brand: "#f0b978"},
		// A client that names a shape names it for both themes, so the document
		// mobile reads has to carry it: ui.Compose renders --pk-radius-* from this
		// same pair, and dropping it would render the client round in the browser
		// and square everywhere else.
		Shape: design.Shape{ButtonRadius: "2px", CardRadius: "0.75rem", ModalRadius: "1.5rem"},
	}.Resolve()
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
	wantDimensions := map[string]string{
		"--pk-radius-button": "2px", "--pk-radius-card": "0.75rem", "--pk-radius-modal": "1.5rem",
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
		gotDimensions := map[string]string{}
		for _, dimension := range mode.Dimensions {
			if dimension.Type != "dimension" {
				t.Errorf("%s exports %v as %q, want a dimension", mode.Mode, dimension.Name, dimension.Type)
			}
			gotDimensions[dimension.Name] = dimension.Value
		}
		// A client's shape reaches the projection both themes carry; one that
		// stopped at the Go value would render the client round in the browser and
		// square on the phone that reads this document.
		if !maps.Equal(gotDimensions, wantDimensions) {
			t.Errorf("%s shape tokens: %v, want %v", mode.Mode, gotDimensions, wantDimensions)
		}
	}
	// The document, not only the struct: this is the artefact platformkit-mobile
	// consumes, and a client's shape that stops at the Go value reaches nobody.
	// The {Modes, Colors} selection is the one a client ships — the whole
	// ExportTokens selection is not DTCG-representable for any pair, this one
	// included (keyword scales and the transition groups are refused by the
	// projection), and that limit belongs to the export owner, not to a theme.
	selection := export.TokenExport{Modes: tokens.Modes, Colors: tokens.Colors}
	document, diagnostics, err := selection.DTCG("light")
	if err != nil {
		t.Fatalf("a generated pair's colour selection is not DTCG-representable: %v (%d diagnostics)", err, len(diagnostics))
	}
	var doc any
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, unit string
		value       float64
	}{
		{"--pk-radius-button", "px", 2},
		{"--pk-radius-card", "rem", 0.75},
		{"--pk-radius-modal", "rem", 1.5},
	} {
		if got := jsonAt(t, doc, "shape", tc.token, "$type"); got != "dimension" {
			t.Errorf("%s projects as %v, want a dimension", tc.token, got)
		}
		if got := jsonAt(t, doc, "shape", tc.token, "$value", "value"); got != tc.value {
			t.Errorf("%s value: %v, want %v", tc.token, got, tc.value)
		}
		if got := jsonAt(t, doc, "shape", tc.token, "$value", "unit"); got != tc.unit {
			t.Errorf("%s unit: %v, want %q", tc.token, got, tc.unit)
		}
	}
	// The whole document, pinned: every colour, family, radius and diagnostic a
	// client's generated identity ships. A change to the projection that is not a
	// decision somebody made moves this number.
	sum := sha256.Sum256(document)
	if got := hex.EncodeToString(sum[:]); got != dtcgGeneratedLightDigest {
		t.Errorf("generated client DTCG(light) digest moved to %s, pinned %s: the projection a client's identity reaches mobile through changed", got, dtcgGeneratedLightDigest)
	}
	// A radius the document cannot carry is refused rather than projected as a
	// string a consumer would have to guess at.
	unprojectable := tokens
	unprojectable.Modes = []export.TokenMode{tokens.Modes[0]}
	unprojectable.Modes[0].Dimensions[1].Value = "50%"
	if _, _, err := (export.TokenExport{Modes: unprojectable.Modes, Colors: tokens.Colors}).DTCG("light"); err == nil {
		t.Errorf("a %% radius reached the document instead of a refusal")
	}
}

// dtcgGeneratedLightDigest is the light-mode document of the generated client
// above, measured at the commit that added the radii. Before that commit the
// same projection carried the same 22 colours and 3 families and no shape at all.
//
// Re-measured for review round 1's HIGH: the document carries the role layer, and
// three of its declarations changed value — --pk-role-fg-secondary and
// --pk-role-fg-tertiary mix text-primary with text-muted now rather than with
// surface-primary, and --pk-role-fg-placeholder is text-muted outright. The 22
// theme colours, the 3 families and the 3 dimensions are the same values; a
// consumer that reads the role layer sees three recolours, and one that reads only
// the theme modes sees no change at all.
//
// Re-measured again for review round 7's HIGH, and this one moves a theme colour:
// the generator now repairs a light-theme status tint against the muted copy a
// tinted panel paints on it, so the warning badge of a generated client is a
// shade paler than it was (light: #f5e3c5-style tints moved a few units toward
// white on the seeds where the muted line measured under 4.5:1). A native screen
// that painted a warning badge sees a shallower tint; nothing else in the
// document moved.
const dtcgGeneratedLightDigest = "d82e4463c91fcb1ef2ea3e1b28aaf3c17ba76cdd7a46434db2bb31c740bf2ddb"

// TestExportTokensRefusesAnAccentThatReadsOnlyOnTheSurfaces is the widened seam
// biting: a pair whose accent clears every surface the theme names, and so clears
// the list of pairs on those surfaces, while failing the tint the role layer
// derives from the accent itself. The narrower list passes it; the export refuses
// it. Both halves are asserted, because a union that dropped the derived pair
// would keep exporting it.
func TestExportTokensRefusesAnAccentThatReadsOnlyOnTheSurfaces(t *testing.T) {
	t.Parallel()
	pair := design.Default()
	// Two surfaces are lifted to white so the card surface binds the accent, which
	// is the situation the tint is derived in: mix(accent, surface-primary).
	pair.Light.SurfaceCanvas, pair.Light.SurfaceMuted = "#ffffff", "#ffffff"
	pair.Light.AccentDefault = "#976a6a" // 4.50:1 on surface-primary, 3.90:1 on its own tint
	if err := pair.Light.CheckRoles(style.RoleColors(), style.BodyRolePairs()); err != nil {
		t.Fatalf("the pairs on the theme's own surfaces: %v", err)
	}
	_, err := export.ExportTokens(pair, "light")
	if err == nil || !strings.Contains(err.Error(), "--pk-role-fg-brand on --pk-role-surface-brand-soft") {
		t.Errorf("an accent legible only on the surfaces exported a document instead of refusing: %v", err)
	}
}
