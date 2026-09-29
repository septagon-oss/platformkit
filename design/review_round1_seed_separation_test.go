package design_test

// Review round 1 (T-0107). Two probes of the separation the delivery says it
// measures: the value pinned in the MinDistance comment, and how often the
// register designconfig.LoadClientDesigns enforces that value refuses a pair of
// ordinary clients. Both cases assert what the constant and the loader claim,
// so each passes once the claim and the measurement agree.

import (
	"fmt"
	"hash/fnv"
	"math"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// corpus draws n deterministic seeds, and returns their generated pairs.
func generatedCorpus(t *testing.T, n int) ([]design.Seed, []design.Pair) {
	t.Helper()
	seeds := make([]design.Seed, 0, n)
	pairs := make([]design.Pair, 0, n)
	for i := range n {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		seed := design.Seed{Sector: "sector-" + decimal(int(h.Sum64()%977)), Name: "identity-" + decimal(i)}
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		seeds = append(seeds, seed)
		pairs = append(pairs, pair)
	}
	return seeds, pairs
}

func decimal(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestMinDistanceHoldsForWellSeparatedSeeds is the delivery's second property,
// stated as the constant states it: "over the 28-seed corpus the closest two
// palettes whose seeds sit 20 degrees or more apart in hue are 0.0145 apart"
// and "A floor of 0.01 admits the first". The case asks the same question over
// a 120-seed corpus, which is what a register of a hundred clients actually
// looks like: two seeds at least 20 degrees apart in hue are never reported
// below the floor design.MinDistance — because a register that refused them
// would refuse clients whose accents are visibly different colours.
func TestMinDistanceHoldsForWellSeparatedSeeds(t *testing.T) {
	const corpusSize = 120
	seeds, pairs := generatedCorpus(t, corpusSize)
	hues := make([]float64, len(seeds))
	for i, seed := range seeds {
		hue, err := seed.Hue()
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		hues[i] = hue
	}
	closest, closestPair := math.MaxFloat64, ""
	refusals := 0
	for i := range pairs {
		for j := i + 1; j < len(pairs); j++ {
			separation := math.Abs(hues[i] - hues[j])
			separation = math.Min(separation, 360-separation)
			if separation < 20 {
				continue
			}
			got := design.Distance(pairs[i], pairs[j])
			if got < closest {
				closest, closestPair = got, seeds[i].Name+" / "+seeds[j].Name
			}
			if design.Colliding(pairs[i], pairs[j]) {
				refusals++
				t.Errorf("client %s (%.1f deg) and client %s (%.1f deg), %d degrees apart in hue, collide: distance %.4f < %.2f — the register refuses them",
					seeds[i].Name, hues[i], seeds[j].Name, hues[j], int(separation), got, design.MinDistance)
			}
		}
	}
	t.Logf("%d seeds, closest well-separated pair %.4f (%s), %d colliding pairs, MinDistance %.2f",
		corpusSize, closest, closestPair, refusals, design.MinDistance)
	if refusals > 0 && closest < design.MinDistance {
		t.Errorf("%d of %d well-separated pairs are refused by the register at %.4f", refusals, corpusSize, closest)
	}
}

// TestCorpusDistanceReportsTheNumbersTheConstantQuotes re-runs the measurement
// written into design/seed.go's comment, so the comment is a claim a check
// holds rather than prose that drifts. It reports both figures the comment
// quotes: the closest two palettes at least 20 degrees apart, and the closest
// pair of distinct seeds in the corpus.
func TestCorpusDistanceReportsTheNumbersTheConstantQuotes(t *testing.T) {
	seeds, pairs := generatedCorpus(t, 120)
	hues := make([]float64, len(seeds))
	for i, seed := range seeds {
		hue, err := seed.Hue()
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		hues[i] = hue
	}
	wellSeparated, nearest := math.MaxFloat64, math.MaxFloat64
	for i := range pairs {
		for j := i + 1; j < len(pairs); j++ {
			got := design.Distance(pairs[i], pairs[j])
			if got < nearest {
				nearest = got
			}
			separation := math.Abs(hues[i] - hues[j])
			separation = math.Min(separation, 360-separation)
			if separation >= 20 && got < wellSeparated {
				wellSeparated = got
			}
		}
	}
	t.Logf("closest at >=20 degrees: %.4f; closest of any two distinct seeds: %.4f; MinDistance %.2f",
		wellSeparated, nearest, design.MinDistance)
	if wellSeparated < design.MinDistance {
		t.Errorf("the comment promises the closest well-separated palettes clear the floor; the corpus puts them at %.4f, below %.2f", wellSeparated, design.MinDistance)
	}
}

// TestFromSeedGeneratesForEveryBrandHue checks the generator's own claim that it
// only refuses "an identity that cannot be made legible". A brand colour fixes
// the hue; nothing in Seed.Validate bounds it, so a hue the generator cannot
// repair is a client that cannot boot. Every hue is a legal brand colour.
func TestFromSeedGeneratesForEveryBrandHue(t *testing.T) {
	for degrees := range 360 {
		brand := hsvLiteral(degrees)
		seed := design.Seed{Sector: "insurance", Name: "Meridian", Brand: brand}
		if _, err := design.FromSeed(seed); err != nil {
			t.Errorf("brand %s (hue %d): FromSeed refuses a client's own brand colour: %v", brand, degrees, err)
		}
	}
}

// hsvLiteral writes the sRGB literal at a hue with the accent's own saturation
// and value, so each hue is tested at equal strength rather than at a grey that
// cannot fail.
func hsvLiteral(degrees int) string {
	h := float64(degrees) / 60
	c := 0.65 * 0.62
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	var r, g, b float64
	switch {
	case h < 1:
		r, g, b = c, x, 0
	case h < 2:
		r, g, b = x, c, 0
	case h < 3:
		r, g, b = 0, c, x
	case h < 4:
		r, g, b = 0, x, c
	case h < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	clamp := func(v float64) int { return max(0, min(255, int(math.Round(v*255)))) }
	return fmt.Sprintf("#%02x%02x%02x", clamp(r+0.19), clamp(g+0.19), clamp(b+0.19))
}
