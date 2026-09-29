package style_test

// The role layer's own gate. TestPaintedRolesClearTheGateTheReadmeClaims (the
// review round that found the gap) measures the resolved colours pair by pair;
// this case measures the same claim through the gate the delivery now ships, so
// the assertion and the API cannot drift apart, and it refuses a role layer that
// does not clear the floor rather than only accepting the one that does.

import (
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestBodyRolePairsClearTheFloorThroughTheGate(t *testing.T) {
	themes := design.Default().Both()
	for i := range 24 {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		seed := design.Seed{Sector: "sector-" + strconv.Itoa(int(h.Sum64()%977)), Name: "identity-" + strconv.Itoa(i)}
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("seed %+v: %v", seed, err)
		}
		themes = append(themes, pair.Both()...)
	}
	for _, theme := range themes {
		if err := theme.CheckRoles(style.RoleColors(), style.BodyRolePairs()); err != nil {
			t.Errorf("%s: role layer: %v", theme.Name, err)
		}
	}
}

// A mute authored as a walk toward the surface under it is the shape that failed:
// legible on that one surface, and on no other. The gate reads it as the browser
// will, so it refuses — and a pair that names a role nobody declares, a floor of
// zero, or a role with alpha in it is refused rather than measured as a number.
func TestCheckRolesRefusesAWashedOutRoleAndAPairThatNamesNoFloor(t *testing.T) {
	theme := design.Light()
	surfacePrimary := design.ColorValue{Reference: "--pk-color-surface-primary"}
	textPrimary := design.ColorValue{Reference: "--pk-color-text-primary"}
	washed := design.ColorValue{Mix: &design.ColorMix{First: textPrimary, FirstPercent: 60, Second: surfacePrimary}}
	roles := append(style.RoleColors(),
		design.ColorToken{Name: "--pk-role-fg-faint", Value: washed},
		design.ColorToken{Name: "--pk-role-fg-veil", Value: design.ColorValue{Literal: "#10101080"}},
	)
	pairs := []design.RolePair{{Foreground: "--pk-role-fg-faint", Background: "--pk-role-surface-tertiary", Min: design.MinContrast}}
	err := theme.CheckRoles(roles, pairs)
	if err == nil || !strings.Contains(err.Error(), "--pk-role-fg-faint on --pk-role-surface-tertiary") || !strings.Contains(err.Error(), "measures") {
		t.Errorf("a foreground walked to 60%% of the text colour passed the role gate: %v", err)
	}
	// The same role on the surface it was walked toward is fine, which is exactly
	// the illusion the pair list has not to fall for.
	if err := theme.CheckRoles(roles, []design.RolePair{{Foreground: "--pk-role-fg-faint", Background: "--pk-role-surface-primary", Min: design.MinContrastGraphic}}); err != nil {
		t.Errorf("the washed role on its own surface: %v", err)
	}
	for _, tc := range []struct {
		name          string
		pairs         []design.RolePair
		want          string
		rolesOverride []design.ColorToken
	}{
		{"undefined role", []design.RolePair{{Foreground: "--pk-role-fg-nothing", Background: "--pk-role-surface-primary", Min: design.MinContrast}}, "does not define", nil},
		{"floor of zero", []design.RolePair{{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-primary"}}, "names floor", nil},
		{"role with alpha", []design.RolePair{{Foreground: "--pk-role-fg-veil", Background: "--pk-role-surface-primary", Min: design.MinContrast}}, "carries alpha", nil},
		{"role outside the theme", []design.RolePair{{Foreground: "--pk-role-fg-faint", Background: "--pk-role-surface-primary", Min: design.MinContrast}}, "role layer",
			[]design.ColorToken{{Name: "--pk-role-fg-faint", Value: design.ColorValue{Reference: "--pk-color-nope"}}}},
	} {
		used := roles
		if tc.rolesOverride != nil {
			used = tc.rolesOverride
		}
		err := theme.CheckRoles(used, tc.pairs)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want it to say %q", tc.name, err, tc.want)
		}
	}
	if got := style.BodyRolePairs(); len(got) < 12 {
		t.Errorf("the body pair list names %d pairs, which cannot cover the roles the components compose", len(got))
	}
}

// The list is the gate, so a body foreground missing from it is painted with no
// measurement behind it. The accent as text is the one that was missing: three
// roles, one colour the theme owns, painted by the kernel's own components onto
// every surface a card can raise itself onto. This case refuses the list dropping
// any of them, which is how the pair gets lost a second time.
func TestBodyRolePairsCoverTheAccentPaintedAsText(t *testing.T) {
	t.Parallel()
	listed := map[string][]string{}
	for _, pair := range style.BodyRolePairs() {
		listed[pair.Foreground] = append(listed[pair.Foreground], pair.Background)
	}
	surfaces := []string{"--pk-role-surface-primary", "--pk-role-surface-secondary", "--pk-role-surface-tertiary"}
	for _, foreground := range []string{"--pk-role-fg-brand", "--pk-role-fg-link", "--pk-role-fg-link-hover"} {
		for _, surface := range surfaces {
			if !slices.Contains(listed[foreground], surface) {
				t.Errorf("%s is never gated against %s: ui/components paints the accent there as body text, and BodyRolePairs lists [%s]",
					foreground, surface, strings.Join(listed[foreground], ", "))
			}
		}
	}
}
