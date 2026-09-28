package design_test

import (
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// corpus is the set of clients the generator is measured against: the slugs in
// the client repository, with a sector each. It is a sample, and the floors the
// tests below assert are the floors measured over it — not a claim that every
// possible seed clears them.
var corpus = []design.Seed{
	{Sector: "shelter", Name: "Pets"}, {Sector: "shelter", Name: "Acme"},
	{Sector: "shelter", Name: "Acmes"}, {Sector: "health", Name: "Havenkit"},
	{Sector: "health", Name: "Velora"}, {Sector: "health", Name: "Lifeos"},
	{Sector: "finance", Name: "Record"}, {Sector: "travel", Name: "Collect"},
	{Sector: "travel", Name: "Cutout"}, {Sector: "retail", Name: "Embed"},
	{Sector: "energy", Name: "Incomum"}, {Sector: "media", Name: "Stockpilot"},
	{Sector: "public", Name: "Tasks"}, {Sector: "health", Name: "Priced Todos"},
	{Sector: "legal", Name: "Services Law"}, {Sector: "logistics", Name: "Stickermule"},
	{Sector: "retail", Name: "Routebook"}, {Sector: "education", Name: "Comumcowork"},
	{Sector: "legal", Name: "Academy"}, {Sector: "travel", Name: "Apex"},
	{Sector: "food", Name: "PlatformKit"}, {Sector: "energy", Name: "Studio"},
	{Sector: "media", Name: "Works"}, {Sector: "public", Name: "Depot"},
	{Sector: "shelter", Name: "Pets", Brand: "#f0b978"},
	{Sector: "health", Name: "Havenkit", Brand: "#0f5d4e"},
	{Sector: "finance", Name: "Record", Brand: "#c2410c"},
	{Sector: "legal", Name: "Academy", Brand: "#1e3a8a"},
}

var generatedHex = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// Property one, and the reason the generator exists at all: no client can be
// handed a palette its own eyes would fail. Every generated pair clears the body
// roles in both themes, and the shipped reference pair clears them too.
func TestGeneratedPairsClearTheContrastGate(t *testing.T) {
	t.Parallel()
	if err := design.Default().Check(); err != nil {
		t.Errorf("the pair this repository ships fails its own gate: %v", err)
	}
	for _, seed := range corpus {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Errorf("seed %s/%s: %v", seed.Sector, seed.Name, err)
			continue
		}
		if err := pair.Check(); err != nil {
			t.Errorf("seed %s/%s: %v", seed.Sector, seed.Name, err)
		}
		for _, theme := range pair.Both() {
			for _, token := range theme.Tokens() {
				if token.Type == "color" && !generatedHex.MatchString(token.Value) {
					t.Errorf("seed %s/%s theme %s emitted %s = %q, want a #rrggbb literal",
						seed.Sector, seed.Name, theme.Name, token.Name, token.Value)
				}
			}
		}
	}
}

// The gate measures, so a reader can check one number rather than trust the
// list: white on black is the 21:1 the standard says it is, and the shipped
// light theme's body text clears 4.5:1 on both of the surfaces it sits on.
func TestContrastMeasuresWhatTheStandardSays(t *testing.T) {
	t.Parallel()
	white, black := design.SRGBA{1, 1, 1, 1}, design.SRGBA{}
	if got := design.Contrast(white, black); got < 20.9 || got > 21.1 {
		t.Errorf("black on white measures %.2f:1, want 21:1", got)
	}
	if got := design.Contrast(white, white); got != 1 {
		t.Errorf("a colour against itself measures %.2f:1, want 1:1", got)
	}
	if got := design.Luminance(black); got != 0 {
		t.Errorf("black's relative luminance is %g, want 0", got)
	}
	light := design.Default().Light
	tokens, err := design.ResolveColors(light.Tokens()[:22], nil)
	if err != nil {
		t.Fatalf("the shipped light theme does not resolve: %v", err)
	}
	text := tokens["--pk-color-text-primary"]
	for _, surface := range []string{"--pk-color-surface-canvas", "--pk-color-surface-primary"} {
		if got := design.Contrast(text, tokens[surface]); got < design.MinContrast {
			t.Errorf("shipped body text on %s measures %.2f:1", surface, got)
		}
	}
}

// Property two: the same seed generates the same pair, byte for byte, however
// many other seeds were generated first or after. A client's identity is
// reviewable because it is reproducible: the seed in design.yaml is the whole
// input, and the diff a client ships is the seed.
func TestFromSeedIsDeterministic(t *testing.T) {
	t.Parallel()
	first, err := design.FromSeed(corpus[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range corpus {
		if _, err := design.FromSeed(seed); err != nil {
			t.Fatal(err)
		}
	}
	again, err := design.FromSeed(corpus[0])
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Errorf("one seed generated two pairs")
	}
	spaced, err := design.FromSeed(design.Seed{Sector: "  Shelter ", Name: "  PETS "})
	if err != nil {
		t.Fatal(err)
	}
	if spaced != first {
		t.Errorf("the same client written two ways generated two pairs")
	}
}

// Property three: two seeds that differ generate palettes a reader tells apart.
// What the generator can promise is not a floor — FNV over two names can land
// them a hundredth of a degree apart in hue — but that a collision is measured
// and named, so the process that would hold both refuses the second. Both halves
// of that are asserted here, over the corpus.
func TestDistinctSeedsGenerateSeparablePalettes(t *testing.T) {
	t.Parallel()
	type generated struct {
		seed design.Seed
		hue  float64
		pair design.Pair
	}
	var all []generated
	for _, seed := range corpus {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatal(err)
		}
		hue, err := seed.Hue()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, generated{seed: seed, hue: hue, pair: pair})
	}
	floor := 1.0
	near := 0
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[i].seed == all[j].seed {
				continue
			}
			got := design.Distance(all[i].pair, all[j].pair)
			if got <= 0 {
				t.Fatalf("seeds %v and %v generated identical palettes", all[i].seed, all[j].seed)
			}
			gap := all[i].hue - all[j].hue
			if gap < 0 {
				gap = -gap
			}
			if gap > 180 {
				gap = 360 - gap
			}
			if gap >= 20 {
				if got < floor {
					floor = got
				}
				continue
			}
			near++
			if !design.Colliding(all[i].pair, all[j].pair) && got < design.MinDistance {
				t.Errorf("Distance reports %.3f for %v and %v but Colliding says they are distinct",
					got, all[i].seed, all[j].seed)
			}
		}
	}
	if floor < design.MinDistance {
		t.Errorf("the closest separable pair in the corpus sits %.4f apart, below the %.2f the register enforces", floor, design.MinDistance)
	}
	if near == 0 {
		t.Skipf("the corpus generated no near neighbours to measure the collision check against")
	}
}

// A seed that attributes nothing generates nothing: no name, no sector, a brand
// colour that is not a colour, or a brand with alpha in it.
func TestFromSeedRefusesASeedThatSaysNothing(t *testing.T) {
	t.Parallel()
	for _, seed := range []design.Seed{
		{Sector: "shelter"},
		{Name: "Pets"},
		{Sector: "shelter", Name: "Pets", Brand: "papayawhip"},
		{Sector: "shelter", Name: "Pets", Brand: "#f0b97880"},
	} {
		pair, err := design.FromSeed(seed)
		if err == nil {
			t.Errorf("seed %+v generated %v", seed, pair.Light.Name)
		}
		if pair != (design.Pair{}) {
			t.Errorf("seed %+v returned a pair with its error", seed)
		}
	}
}

// The focus ring is not the accent, whichever seed generated the pair: the ring
// wears the accent's complement.
func TestGeneratedFocusRingIsNotTheAccent(t *testing.T) {
	t.Parallel()
	for _, seed := range corpus {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatal(err)
		}
		for _, theme := range pair.Both() {
			if theme.Focus == theme.AccentDefault {
				t.Errorf("seed %s/%s theme %s rings its controls in the control", seed.Sector, seed.Name, theme.Name)
			}
		}
	}
}
