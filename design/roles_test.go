package design_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// roles.go is the reason one gate is enough. This is the disagreement that made
// it necessary: a dark theme whose card surface is the warm neutral #2e2920 — an
// override a brand team writes, and one every token pair survives — moves
// --pk-role-surface-brand-soft, 12 % of the accent mixed into that surface, out
// from under the accent painted on it. The token half accepts the pair; the role
// half measures the brand badge's own label at 3.91:1. Round 5 swept the override
// space this branch opened and found 98 such literals; this pins one of them, so
// the two halves cannot drift back apart without a case going red.
func TestTheRoleHalfRefusesAPairTheTokenHalfAccepts(t *testing.T) {
	t.Parallel()
	pair, err := design.FromSeed(design.Seed{Sector: "insurance", Name: "Meridian"})
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	pair.Dark.SurfacePrimary = "#2e2920"
	if err := pair.Check(); err != nil {
		t.Fatalf("the case measures nothing: the token half already refuses it: %v", err)
	}
	err = pair.CheckRoles(design.RoleLayer(), design.GatedRolePairs())
	if err == nil {
		t.Fatal("the role half accepted the pair the export refuses")
	}
	if !strings.Contains(err.Error(), "--pk-role-fg-brand") ||
		!strings.Contains(err.Error(), "--pk-role-surface-brand-soft") {
		t.Errorf("the refusal names neither painted role: %v", err)
	}
}

// Every role a gated pair names has to be a role the layer declares: a pair that
// drifted away from the declarations would be measured against nothing, and
// CheckRoles would say the layer is legible. The list names roles as roles.go
// declares them and the gate resolves the name a sheet emits, so this is the same
// translation CheckRoles performs, not a spelling the test invents.
func TestGatedRolePairsMeasureDeclaredRoles(t *testing.T) {
	t.Parallel()
	declared := map[string]bool{}
	for _, role := range design.RoleLayer() {
		if declared[role.Name] {
			t.Fatalf("%s is declared twice", role.Name)
		}
		declared[role.Name] = true
	}
	gated := design.GatedRolePairs()
	if want := len(design.BodyRolePairs()) + len(design.TintedRolePairs()) + len(design.StatusRolePairs()); len(gated) != want || len(gated) < 29 {
		t.Fatalf("the gated list is %d pairs, want the %d body pairs plus the %d derived ones plus the %d tinted-status ones",
			len(gated), len(design.BodyRolePairs()), len(design.TintedRolePairs()), len(design.StatusRolePairs()))
	}
	for _, pair := range gated {
		for _, name := range [...]string{pair.Foreground, pair.Background} {
			if !declared[design.RoleCSSName(name)] {
				t.Errorf("the gate measures %s, which the layer does not declare", name)
			}
		}
		if pair.Min <= 0 {
			t.Errorf("%s on %s names floor %v", pair.Foreground, pair.Background, pair.Min)
		}
	}
}

// A mix is a pointer, so the declarations have to be detached on the way out.
// The case that measures a shallower tint than the layer declares rewrites a
// percentage in what it was handed; a shared table would move the layer every
// later caller in the process measures — including this package's own gate.
func TestRoleLayerHandsOutADetachedDeclaration(t *testing.T) {
	t.Parallel()
	first := design.RoleLayer()
	roles := map[string]design.ColorToken{}
	for _, role := range first {
		roles[role.Name] = role
	}
	tint, surface := roles["--pk-role-surface-brand-soft"], roles["--pk-role-fg-secondary"]
	if tint.Value.Mix == nil || surface.Value.Mix == nil {
		t.Fatal("a derived role lost its mix")
	}
	tint.Value.Mix.FirstPercent = 90
	surface.Value.Mix.FirstPercent = 100
	second := design.RoleLayer()
	for _, role := range second {
		switch role.Name {
		case "--pk-role-surface-brand-soft":
			if role.Value.Mix.FirstPercent != design.SoftTintPercent {
				t.Errorf("the brand tint moved to %v %%, it is owned by SoftTintPercent", role.Value.Mix.FirstPercent)
			}
		case "--pk-role-fg-secondary":
			if role.Value.Mix.FirstPercent != 66 {
				t.Errorf("fg-secondary moved to %v %% of text-primary", role.Value.Mix.FirstPercent)
			}
		}
	}
	pairs := design.GatedRolePairs()
	pairs[0].Min = 1
	if design.BodyRolePairs()[0].Min != design.MinContrast {
		t.Error("a caller editing the gated list moved the declaration")
	}
}

// A gate handed nothing to measure is a gate nobody ran, and a caller that lost
// its list would otherwise be told the layer is legible.
func TestCheckRolesRefusesACallerThatNamesNoPair(t *testing.T) {
	t.Parallel()
	err := design.Default().Dark.CheckRoles(design.RoleLayer(), nil)
	if err == nil || !strings.Contains(err.Error(), "no pair") {
		t.Errorf("an empty pair list reached the colours: %v", err)
	}
}

// TestCheckRolesRefusesARoleNeitherVocabularyNames is the price of accepting a
// pair written in either vocabulary: a name in neither still has to be refused.
// A role that resolved to nothing would leave the pair measured against the zero
// colour, which reads as black on black and would be refused as 1.00:1 for the
// wrong reason — and a token name is exactly that mistake, since the layer names
// roles and a theme names tokens.
func TestCheckRolesRefusesARoleNeitherVocabularyNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fg-nowhere", "--pk-role-fg-nowhere", "text-muted", ""} {
		err := design.Default().Light.CheckRoles(design.RoleLayer(), []design.RolePair{
			{Foreground: name, Background: "surface-primary", Min: design.MinContrast},
		})
		if err == nil || !strings.Contains(err.Error(), "does not define") {
			t.Errorf("CheckRoles accepted the role name %q: %v", name, err)
		}
	}
}

// TestCheckRolesMeasuresTheSamePairInEitherSpelling is why the two vocabularies
// are one pair rather than two gates: the foreground a component writes and the
// foreground a stylesheet emits have to agree, refusal and all, or a palette could
// be steered by how a caller happened to spell a role.
func TestCheckRolesMeasuresTheSamePairInEitherSpelling(t *testing.T) {
	t.Parallel()
	pair, err := design.FromSeed(design.Seed{Sector: "insurance", Name: "Meridian"})
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	pair.Dark.SurfacePrimary = "#2e2920"
	measured := func(foreground, background string) error {
		return pair.Dark.CheckRoles(design.RoleLayer(), []design.RolePair{
			{Foreground: foreground, Background: background, Min: design.MinContrast},
		})
	}
	declared := measured("fg-brand", "surface-brand-soft")
	emitted := measured("--pk-role-fg-brand", "--pk-role-surface-brand-soft")
	if declared == nil || emitted == nil {
		t.Fatalf("the warm card surface was meant to move the badge's own label under the floor: declared %v, emitted %v", declared, emitted)
	}
	if declared.Error() != emitted.Error() {
		t.Errorf("one pair in two spellings gave two refusals:\n%v\n%v", declared, emitted)
	}
	if !strings.Contains(declared.Error(), "--pk-role-fg-brand") {
		t.Errorf("the refusal did not name the property a reader finds in the sheet: %v", declared)
	}
}
