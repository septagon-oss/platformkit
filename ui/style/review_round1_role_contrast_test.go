package style_test

// Review round 1 (T-0107). The delivery's README claims "Check is WCAG 2.2
// measured by Luminance and Contrast: every body role at 4.5:1 (SC 1.4.3)".
// What design.Theme.Check actually measures is 16 pairs of *tokens*
// (--pk-color-*). The layer a browser paints is the *role* layer, and this
// package mutes the tokens before painting them: fg-secondary is
// mix(text-primary 78%, surface-primary) and fg-tertiary is
// mix(text-primary 60%, surface-primary), and components compose those muted
// roles onto the muted surface (ui/components/classlists.go: "neutral" badge =
// Bg(SurfaceTertiary).TextColor(FgSecondary)). No pair the gate reads is that
// pair. This case asks the question the README answers: does the text the
// kernel actually paints reach the number the kernel says it gates, for the
// palette design.Default() ships and for the palettes design.FromSeed
// generates? It asserts the correct behaviour, so it passes once the gate
// measures the resolved roles (or the mixes are brought back above the floor)
// and it prints the role, the surface and the measured ratio while it fails.

import (
	"hash/fnv"
	"math"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// rolePairs are foreground/background role pairs that ui/components composes,
// read out of roleValues() and classlists.go, all of them body-size text.
var rolePairs = []struct {
	foreground, background string
}{
	{"--pk-role-fg-primary", "--pk-role-surface-primary"},
	{"--pk-role-fg-primary", "--pk-role-surface-secondary"},
	{"--pk-role-fg-primary", "--pk-role-surface-tertiary"},
	{"--pk-role-fg-secondary", "--pk-role-surface-primary"},
	{"--pk-role-fg-secondary", "--pk-role-surface-secondary"},
	{"--pk-role-fg-secondary", "--pk-role-surface-tertiary"},
	{"--pk-role-fg-tertiary", "--pk-role-surface-primary"},
	{"--pk-role-fg-tertiary", "--pk-role-surface-secondary"},
	{"--pk-role-fg-tertiary", "--pk-role-surface-tertiary"},
}

// hashedSeeds is a deterministic corpus: no name here is a client, and the
// corpus is not chosen to make the gate look good — it is the first N of a hash.
func hashedSeeds(n int) []design.Seed {
	seeds := make([]design.Seed, 0, n)
	for i := range n {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		seeds = append(seeds, design.Seed{
			Sector: "sector-" + itoa(int(h.Sum64()%977)),
			Name:   "identity-" + itoa(i),
		})
	}
	return seeds
}

func itoa(n int) string {
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

func checkRoles(t *testing.T, label string, theme design.Theme) {
	t.Helper()
	colors, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
	if err != nil {
		t.Fatalf("%s %s: resolve roles: %v", label, theme.Name, err)
	}
	for _, pair := range rolePairs {
		foreground, background := colors[pair.foreground], colors[pair.background]
		got := design.Contrast(foreground, background)
		if got < design.MinContrast || math.IsNaN(got) {
			t.Errorf("%s %s: %s on %s measures %.2f:1, below the %.1f:1 design/README.md says every body role reaches (%s %s)",
				label, theme.Name, pair.foreground, pair.background, got, design.MinContrast,
				pair.foreground, pair.background)
		}
	}
}

// TestPaintedRolesClearTheGateTheReadmeClaims is the case: every role pair a
// component composes at body size reaches design.MinContrast, in both themes,
// for the shipped palette and for generated ones.
func TestPaintedRolesClearTheGateTheReadmeClaims(t *testing.T) {
	for _, theme := range design.Default().Both() {
		checkRoles(t, "design.Default()", theme)
	}
	for _, seed := range hashedSeeds(40) {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		if err := pair.Check(); err != nil {
			t.Fatalf("seed %+v generated a pair its own gate refuses: %v", seed, err)
		}
		for _, theme := range pair.Both() {
			checkRoles(t, "FromSeed{"+seed.Name+", "+seed.Sector+"}", theme)
		}
	}
}

// TestPaintedRolesAreNotTheTokensTheGateMeasures names the gap in the delivery's
// own words: the role layer is not a projection of the tokens it checks, so a
// green Theme.Check says nothing about these pairs. A passing
// design.Theme.Check together with a measured ratio under the floor is the
// finding; the day the gate reads the roles, both halves agree and this passes.
func TestPaintedRolesAreNotTheTokensTheGateMeasures(t *testing.T) {
	for _, theme := range design.Default().Both() {
		if err := theme.Check(); err != nil {
			t.Fatalf("the shipped theme must pass its own gate for this case to mean anything: %v", err)
		}
		colors, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"--pk-role-fg-secondary", "--pk-role-fg-tertiary"} {
			if !strings.HasPrefix(role, "--pk-role-") {
				t.Fatal("the role layer is the layer under test")
			}
			if design.Luminance(colors[role]) == design.Luminance(colors["--pk-role-fg-primary"]) {
				t.Errorf("%s of the %s theme resolves to the primary foreground: the mute this package authors has gone, and the case below no longer measures it",
					role, theme.Name)
			}
		}
	}
}
