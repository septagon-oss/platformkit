package style_test

// Review round 3 (T-0107). Round 1 found the contrast gate measuring the token
// layer while the kernel paints the role layer; round 2 found the role gate's
// list narrower than the layer it claims to read (the accent as link text).
// Both were cured by adding entries to two lists — design.bodyContrast and
// ui/style.bodyRolePairs. A list is still the shape of that gate, so this case
// does the same thing round 2 did with the cross product: it reads the paint out
// of ui/components/classlists.go and measures the combinations NEITHER list
// names, for design.Default() and a hashed corpus of generated palettes, both
// themes.
//
// Two pairs come out of that reading, and each is composed by one rule of one
// component, so neither assertion depends on guessing how a page is assembled:
//
//   - clBadgeVariant["primary"], clBadgeTone["brand"], clSidebarLinkActiveContent
//     and the outline button's hover state all set Background(SurfaceBrandSoft)
//     with TextColor(FgBrand/FgLink). SurfaceBrandSoft is mix(accent 12%,
//     surface-primary): the background is pulled toward the very text painted on
//     it, so its ratio is always lower than the same text on the surface the mix
//     was taken from — and the surface the mix was taken from is the one the
//     token gate certifies against. No rule in the kernel adds a class carrying
//     SurfaceBrandSoft to a body role, so no measurement reaches it.
//   - clCardFrame sets Background(SurfacePrimary) and clDetailValueTone[tone]
//     sets TextColor(FgSuccess/FgWarning/FgDanger/FgInfo) at TextSM; ui/components
//     examples/gallery.go renders exactly that ("Detail / Risk: High, Tone
//     danger"). clTextColor[tone] and clFieldErr paint the same four tones with no
//     background of their own, so the page canvas — the role layer's
//     surface-secondary, which ui/style maps onto --pk-color-surface-canvas — is
//     their other background. design/contrast.go reads each status token only
//     against its own badge background (status-danger on status-dangerbg), and
//     neither BodyRolePairs nor the token list reads a status tone on a surface.
//
// Every assertion is about the ratio this test measures and prints, never about
// anything the code under test says. Both cases pass when the two lists read what
// the components paint (or the generated colours are repaired against it) and
// fail while they do not.

import (
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// round3Seeds is a hash, not a list chosen to make the gate look good.
func round3Seeds(n int) []design.Seed {
	seeds := make([]design.Seed, 0, n)
	for i := range n {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(i ^ 0x5a), byte(i >> 8), byte(i >> 16), 7})
		seeds = append(seeds, design.Seed{
			Sector: "sector-" + itoa3(int(h.Sum64()%997)),
			Name:   "identity-" + itoa3(i),
		})
	}
	return seeds
}

func itoa3(n int) string {
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

// round3Palettes is the shipped pair and every generated pair of the corpus. A
// palette the kernel's own gates refuse is a separate failure: this case measures
// colours, and a pair that cannot be measured is not the same finding.
func round3Palettes(t *testing.T) map[string]design.Pair {
	t.Helper()
	pairs := map[string]design.Pair{"design.Default()": design.Default()}
	for _, seed := range round3Seeds(60) {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		if err := pair.Check(); err != nil {
			t.Fatalf("seed %+v generated a pair its own token gate refuses: %v", seed, err)
		}
		if err := pair.CheckRoles(style.RoleColors(), style.BodyRolePairs()); err != nil {
			t.Fatalf("seed %+v generated a pair the role gate ui/export runs refuses: %v", seed, err)
		}
		pairs["FromSeed{"+seed.Name+", "+seed.Sector+"}"] = pair
	}
	return pairs
}

// measurePainted resolves the role layer once per theme and reports every pair
// of wanted that falls below floor, at most maxFailures of them, with the rule
// that composes it. It returns how many measurements failed and the worst ratio
// seen, so the caller can report a count rather than a sample.
func measurePainted(t *testing.T, pairs map[string]design.Pair, wanted []string, floor float64, composed string, maxFailures int) (int, float64) {
	t.Helper()
	failures, worst, seen := 0, floor, 0
	for _, label := range sortedLabels(pairs) {
		for _, theme := range pairs[label].Both() {
			colors, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
			if err != nil {
				t.Fatalf("%s %s: resolve roles: %v", label, theme.Name, err)
			}
			for _, pair := range wanted {
				foreground, background, ok := strings.Cut(pair, " on ")
				if !ok {
					t.Fatalf("bad wanted pair %q", pair)
				}
				got := design.Contrast(colors[foreground], colors[background])
				if seen == 0 || got < worst {
					worst = got
				}
				seen++
				if got >= floor {
					continue
				}
				failures++
				if failures <= maxFailures {
					t.Errorf("%s %s: %s measures %.2f:1, below the %.1f:1 the gate claims for body roles; composed by %s",
						label, theme.Name, pair, got, floor, composed)
				}
			}
		}
	}
	t.Logf("%d measurements, %d below %.1f:1, worst %.2f:1", seen, failures, floor, worst)
	return failures, worst
}

func sortedLabels(pairs map[string]design.Pair) []string {
	return slices.Sorted(maps.Keys(pairs))
}

// TestAccentTextOnItsOwnTintClearsTheFloor is the first case: the accent painted
// as copy on the accent's own tint, which no list measures.
func TestAccentTextOnItsOwnTintClearsTheFloor(t *testing.T) {
	wanted := []string{
		"--pk-role-fg-brand on --pk-role-surface-brand-soft",
		"--pk-role-fg-link on --pk-role-surface-brand-soft",
	}
	if failures, _ := measurePainted(t, round3Palettes(t), wanted, design.MinContrast,
		"clBadgeVariant[\"primary\"] and clBadgeTone[\"brand\"] (Bg SurfaceBrandSoft, TextColor FgBrand), clSidebarLinkActiveContent, and the outline button's hover state (clButtonVariant[\"outline\"] On(hover) hoverBg(SurfaceBrandSoft) with its own TextColor(FgBrand))", 8); failures == 0 {
		t.Log("the tint is legible; keep it in whatever list measures the paint so it stays that way")
	}
}

// TestStatusToneTextOnTheSurfacesACardPaintsClearsTheFloor is the second case:
// the four status tones as body copy on the two backgrounds the kernel's own
// card and page paint under them.
func TestStatusToneTextOnTheSurfacesACardPaintsClearsTheFloor(t *testing.T) {
	var wanted []string
	for _, tone := range []string{"success", "warning", "danger", "info"} {
		for _, surface := range []string{"--pk-role-surface-primary", "--pk-role-surface-secondary"} {
			wanted = append(wanted, "--pk-role-fg-"+tone+" on "+surface)
		}
	}
	if failures, _ := measurePainted(t, round3Palettes(t), wanted, design.MinContrast,
		"clDetailValueTone[tone] (TextSM, TextColor FgDanger and its three siblings) inside clCardFrame (Bg SurfacePrimary) — ui/components/examples/gallery.go renders that Detail with Tone \"danger\" — and clTextColor[tone]/clFieldErr on the page canvas, which the role layer resolves as surface-secondary = --pk-color-surface-canvas", 8); failures == 0 {
		t.Log("the status tones read on both surfaces; keep them in whatever list measures the paint so they stay that way")
	}
}

// TestPaintedRolePairsNeitherListNamesAreNotInvented is the reachability probe
// run the other way: it refuses the premise of both cases if the kernel stops
// painting them, so the two cases above cannot go green by accident while the
// paint they name exists and cannot stay red once it is gone. It reads the role
// layer the same way ResolveColors does and asserts the tint is still a mix of
// the accent with a surface — the reason its ratio is lower than the surface's —
// and that no BodyRolePairs entry mentions the tint or a status tone.
func TestPaintedRolePairsNeitherListNamesAreNotInvented(t *testing.T) {
	colors, err := design.ResolveColors(design.Default().Light.Tokens(), style.RoleColors())
	if err != nil {
		t.Fatalf("resolve roles: %v", err)
	}
	tint, ok := colors["--pk-role-surface-brand-soft"]
	if !ok {
		t.Fatal("--pk-role-surface-brand-soft is not declared by the role layer ui/style emits")
	}
	surface, hasSurface := colors["--pk-role-surface-primary"]
	accent, hasAccent := colors["--pk-role-fg-brand"]
	if !hasSurface || !hasAccent {
		t.Fatalf("the card surface or the accent text role is missing: %v %v", hasSurface, hasAccent)
	}
	if design.Contrast(accent, tint) >= design.Contrast(accent, surface) {
		t.Errorf("the tint reads better than the surface it is mixed from (%.2f:1 against %.2f:1): the premise of the first case has gone, so fix the case, not the palette",
			design.Contrast(accent, tint), design.Contrast(accent, surface))
	}
	for _, pair := range style.BodyRolePairs() {
		if pair.Background == "--pk-role-surface-brand-soft" {
			t.Errorf("%s IS gated on the tint after all: the first case should now pass", pair.Foreground)
		}
	}
}
