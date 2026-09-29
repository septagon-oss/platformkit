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
// finished pair: an override that breaks a gated token pair is refused whole, with
// the ratio it measured, and resolves to no pair. (The role layer's half of the
// same gate is TestClientResolveRefusesAnOverrideTheRoleLayerRefuses below.)
func TestClientResolveRefusesAnOverrideThatBreaksAGatedTokenPair(t *testing.T) {
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

// A status tone is painted as copy, not only inside its own badge: a detail
// value and a field's error line put it on whatever surface the card around them
// raised itself onto. An override that keeps the badge pair legible and drags the
// same token under the floor on a raised card is therefore refused too — the
// refusal names the surface, which is how a reader of design.yaml learns that the
// badge was never the only place the colour had to read.
func TestClientResolveRefusesAStatusOverrideThatReadsOnItsBadgeOnly(t *testing.T) {
	t.Parallel()
	client := design.Client{
		Slug:   "amber",
		Seed:   design.Seed{Sector: "retail", Name: "Pale"},
		Tokens: map[string]map[string]string{"light": {"status-warning": "#7b4e02"}},
	}
	pair, err := client.Resolve()
	if err == nil {
		t.Fatalf("resolved a client whose warning text reads 4.42:1 on a raised card: %v", pair)
	}
	if pair != (design.Pair{}) {
		t.Errorf("a refused client returned a pair")
	}
	if !strings.Contains(err.Error(), "status-warning on surface-muted") {
		t.Errorf("refusal does not name the surface that failed: %v", err)
	}
	if strings.Contains(err.Error(), "status-warningbg") {
		t.Errorf("refusal blames the badge, which this override leaves legible: %v", err)
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

// The token half is not the door. These three literals came out of the sweep
// review round 5 ran over the override space this branch opened: each holds all
// 22 token pairs, and each drops a pair the role layer composes under the floor —
// the accent on the tint mixed from it, and text-primary's own mixed role on the
// surface a card raises itself onto. Resolve owns the whole gate, so a client's
// own file is refused at this door and not only later, where a Pair becomes a
// stylesheet, which is what makes the two seams agree about one palette.
func TestClientResolveRefusesAnOverrideTheRoleLayerRefuses(t *testing.T) {
	t.Parallel()
	seed := design.Seed{Sector: "insurance", Name: "Meridian"}
	for _, override := range []struct{ theme, token, value string }{
		{"dark", "surface-primary", "#2e2920"},
		{"dark", "accent-default", "#f46218"},
		{"dark", "text-primary", "#fa19fa"},
	} {
		client := design.Client{Slug: "meridian", Seed: seed,
			Tokens: map[string]map[string]string{override.theme: {override.token: override.value}}}
		pair, err := client.Resolve()
		if err == nil {
			t.Fatalf("%s.%s=%s resolved: the role layer never reached this door",
				override.theme, override.token, override.value)
		}
		if pair != (design.Pair{}) {
			t.Errorf("%s.%s=%s: a refused client returned a pair", override.theme, override.token, override.value)
		}
		// The refusal is the gate's, at the role layer's own words: the role it
		// measured, the ratio it measured and the floor it fell under.
		if !strings.Contains(err.Error(), "--pk-role-") || !strings.Contains(err.Error(), "4.5:1") {
			t.Errorf("%s.%s=%s: refusal names no role and no floor: %v",
				override.theme, override.token, override.value, err)
		}
	}
}

// A tint is not only the badge it was named for: ui/components gives a failed
// media panel the warning ground and fills it with the muted reason line, so a
// client that ships a deeper tint than the generator draws moves a sentence no
// token pair measures. This override holds every one of the 22 token pairs and
// every body and brand-tinted role pair, and paints that muted line at 3.75:1 —
// which is what the status section of the gated list is for.
func TestClientResolveRefusesATintThatMovesTheMutedLineOnIt(t *testing.T) {
	t.Parallel()
	client := design.Client{
		Slug:   "sand",
		Seed:   design.Seed{Sector: "legal", Name: "Client211"},
		Tokens: map[string]map[string]string{"light": {"status-warningbg": "#d0ccbc"}},
	}
	pair, err := client.Resolve()
	if err == nil {
		t.Fatalf("resolved a client whose tinted panel nobody can read: bg %q, muted %q",
			pair.Light.StatusWarningBg, pair.Light.TextMuted)
	}
	if pair != (design.Pair{}) {
		t.Errorf("a refused client returned a pair")
	}
	if !strings.Contains(err.Error(), "--pk-role-fg-muted") ||
		!strings.Contains(err.Error(), "--pk-role-surface-warning-soft") {
		t.Errorf("the refusal names neither the muted line nor the tint under it: %v", err)
	}
	// The same file with the tint left alone resolves: the refusal is this
	// override, not the client's identity.
	dropped := client
	dropped.Tokens = nil
	if _, err := dropped.Resolve(); err != nil {
		t.Errorf("the client's own seed refuses: %v", err)
	}
}
