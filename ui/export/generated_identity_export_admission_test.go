package export_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
)

// TestEveryIdentityTheGeneratorIssuesIsAdmittedByTheTokenExport holds the two
// gates of a generated identity to one answer. design.FromSeed certifies a pair
// against design's role list; ExportTokens refuses a pair against ui/style's
// spelling of it, at the seam where the pair becomes the document
// platformkit-mobile reads. A pair the generator issued and the export then
// refused would be an identity a client can run in the browser and never ship
// to a phone. The sweep reads unbranded seeds across sectors and a brand hue
// every 15 degrees at three lightnesses, and every pair it issues must export
// in both modes with the theme's own colour values, unchanged.
func TestEveryIdentityTheGeneratorIssuesIsAdmittedByTheTokenExport(t *testing.T) {
	t.Parallel()
	var seeds []design.Seed
	for i := range 160 {
		seeds = append(seeds, design.Seed{Sector: fmt.Sprintf("sector-%d", i%53), Name: fmt.Sprintf("identity-%d", i)})
	}
	for hue := 0; hue < 360; hue += 15 {
		for _, lightness := range []float64{0.25, 0.5, 0.75} {
			brand := brandHex(float64(hue), 0.7, lightness)
			seeds = append(seeds, design.Seed{Sector: "sector-brand", Name: "branded-" + brand, Brand: brand})
		}
	}
	issued := 0
	for _, seed := range seeds {
		pair, err := design.FromSeed(seed)
		if err != nil {
			// A seed the generator cannot make legible is refused with no pair,
			// which is the generator's own contract; the export never sees it.
			continue
		}
		issued++
		tokens, err := export.ExportTokens(pair, "light", "dark")
		if err != nil {
			t.Errorf("FromSeed issued %+v, and the token export refuses it: %v", seed, err)
			continue
		}
		if len(tokens.Modes) != 2 {
			t.Errorf("%+v exports %d modes, want light and dark", seed, len(tokens.Modes))
			continue
		}
		for i, mode := range tokens.Modes {
			want := map[string]string{}
			for _, token := range pair.Both()[i].Tokens() {
				if token.Type == "color" {
					want[token.Name] = token.Value
				}
			}
			if len(mode.Colors) != len(want) {
				t.Errorf("%+v %s exports %d colours, the theme owns %d", seed, mode.Mode, len(mode.Colors), len(want))
			}
			for _, color := range mode.Colors {
				if want[color.Name] != color.Value {
					t.Errorf("%+v %s exports %s as %q, the theme holds %q", seed, mode.Mode, color.Name, color.Value, want[color.Name])
				}
			}
		}
	}
	if issued < len(seeds)/2 {
		t.Fatalf("the generator issued %d of %d seeds; the sweep measured too few identities to say anything", issued, len(seeds))
	}
}

// brandHex spells an HSL colour (hue in degrees, saturation and lightness in
// [0,1]) as the #rrggbb literal a seed's brand accepts.
func brandHex(hue, saturation, lightness float64) string {
	chroma := (1 - math.Abs(2*lightness-1)) * saturation
	x := chroma * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	var r, g, b float64
	switch {
	case hue < 60:
		r, g = chroma, x
	case hue < 120:
		r, g = x, chroma
	case hue < 180:
		g, b = chroma, x
	case hue < 240:
		g, b = x, chroma
	case hue < 300:
		r, b = x, chroma
	default:
		r, b = chroma, x
	}
	m := lightness - chroma/2
	channel := func(v float64) int { return int(math.Round((v + m) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", channel(r), channel(g), channel(b))
}
