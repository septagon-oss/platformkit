package design_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// A client's design.yaml is the whole of how a client changes the product's
// appearance: a seed, at most named tokens, and its own font stacks and radii.
// Everything this test refuses is a client trying to ship a rule instead.
func TestClientResolveAppliesItsOwnTokensAndStaysLegible(t *testing.T) {
	t.Parallel()
	client := design.Client{
		Slug: "pets",
		Seed: design.Seed{Sector: "shelter", Name: "Pets", Brand: "#f0b978"},
		Tokens: map[string]map[string]string{
			"light": {"accent-default": "#7a4a12", "accent-on": "#fff8ef"},
		},
		Typography: design.Typography{Display: "Iowan Old Style, Georgia, serif"},
		Shape:      design.Shape{CardRadius: "0.75rem"},
	}
	pair, err := client.Resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pair.Light.AccentDefault != "#7a4a12" || pair.Light.AccentOn != "#fff8ef" {
		t.Errorf("client tokens did not reach the pair: %q %q", pair.Light.AccentDefault, pair.Light.AccentOn)
	}
	if pair.Dark.AccentDefault == pair.Light.AccentDefault {
		t.Errorf("a light-theme token leaked into the dark theme")
	}
	if pair.Light.Shape.CardRadius != "0.75rem" || pair.Dark.Shape.CardRadius != "0.75rem" {
		t.Errorf("shape did not reach both themes: %+v", pair.Light.Shape)
	}
	// The type pairing is the client's, and the rest keeps the shipped stacks.
	if pair.Light.Typography.Display != "Iowan Old Style, Georgia, serif" {
		t.Errorf("display stack: %q", pair.Light.Typography.Display)
	}
	var mono string
	for _, token := range pair.Light.Tokens() {
		if token.Name == "--pk-font-mono" {
			mono = token.Value
		}
	}
	if !strings.Contains(mono, "monospace") {
		t.Errorf("unset mono stack should keep the shipped default, got %q", mono)
	}
	if err := pair.Check(); err != nil {
		t.Errorf("resolved pair: %v", err)
	}
}

// The gate is the same one FromSeed runs its own output through, applied to the
// finished pair: an override that breaks a body role is refused whole, with the
// ratio it measured, and resolves to no pair.
func TestClientResolveRefusesAnOverrideThatBreaksAGatedRole(t *testing.T) {
	t.Parallel()
	client := design.Client{
		Slug:   "pale",
		Seed:   design.Seed{Sector: "retail", Name: "Pale"},
		Tokens: map[string]map[string]string{"light": {"text-muted": "#d8d2c8"}},
	}
	pair, err := client.Resolve()
	if err == nil {
		t.Fatalf("resolved an unreadable client: %q on %q", pair.Light.TextMuted, pair.Light.SurfaceCanvas)
	}
	if pair != (design.Pair{}) {
		t.Errorf("a refused client returned a pair")
	}
	if !strings.Contains(err.Error(), "text-muted") || !strings.Contains(err.Error(), "4.5:1") {
		t.Errorf("refusal names neither the token nor the floor: %v", err)
	}
}

func TestClientValidateRefusesWhatTheKernelHasNoMeaningFor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		client design.Client
		want   string
	}{
		{"no slug", design.Client{Seed: design.Seed{Sector: "retail", Name: "X"}}, "requires a slug"},
		{"no seed", design.Client{Slug: "x", Seed: design.Seed{Name: "X"}}, "requires a sector"},
		{"other theme", design.Client{Slug: "x", Seed: design.Seed{Sector: "retail", Name: "X"},
			Tokens: map[string]map[string]string{"brand": {"focus": "#0f5d4e"}}}, "want light or dark"},
		{"token nothing exports", design.Client{Slug: "x", Seed: design.Seed{Sector: "retail", Name: "X"},
			Tokens: map[string]map[string]string{"light": {"sidebar-width": "#e0d8cc"}}}, "which no theme exports"},
		{"value that is not a colour", design.Client{Slug: "x", Seed: design.Seed{Sector: "retail", Name: "X"},
			Tokens: map[string]map[string]string{"light": {"focus": "rebeccapurple"}}}, "unsupported colour literal"},
		{"a declaration as a font stack", design.Client{Slug: "x", Seed: design.Seed{Sector: "retail", Name: "X"},
			Typography: design.Typography{Body: "Georgia, serif; body { display: none }"}}, "not one"},
		{"a declaration as a radius", design.Client{Slug: "x", Seed: design.Seed{Sector: "retail", Name: "X"},
			Shape: design.Shape{CardRadius: "calc(1rem + 1vw)"}}, "not one"},
	} {
		err := tc.client.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}
