package style_test

// Review round 5 (T-0107). The brief's Specify §3 says a client's design.yaml
// "carries the seed and at most named token overrides, each validated by the
// same contrast gate", and design/README.md says "Client.Resolve runs the same
// gate over the finished pair". The gate this branch built has two halves:
// design.Pair.Check measures the 22 --pk-color-* tokens a theme sets, and
// Theme.CheckRoles measures the --pk-role-* layer ui/style composes on top of
// them, over the pairs BodyRolePairs() and TintedRolePairs() name. ui/export
// runs both halves where a pair becomes a stylesheet; design.Client.Resolve —
// the door a client's file actually passes through, and the only public route
// from a named override to the Pair ui.Compose takes — runs the first half only.
//
// The first case measures the disagreement, in one process, over three legal
// overrides found by sweeping the override space this branch opened (of 23115
// overrides Client.Resolve accepts, 98 have a gated role pair below the floor:
// 87 from dark.surface-primary, 10 from dark.accent-default, 1 from
// dark.text-primary). It is satisfied by the behaviour the brief specifies —
// Resolve refusing an override whose finished pair the export would refuse — or
// by the finished pair holding the floor. A gate added only at ui.Compose would
// leave the client's own file accepted, which is the thing §3 refuses.
//
// Every case is reached through what the fixed behaviour prints: the literal is
// checked against the *unoverridden* seed and against Client.Validate, neither
// of which is the thing under test, so the case still says something the day
// Resolve refuses. The second case is the pin: it asserts the halves agree about
// a pair nobody overrode, and it passes at these bytes.

import (
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// meridianSeed is the seed the delivery measures in its own report, so this
// case starts from a palette the branch already claims to certify.
var meridianSeed = design.Seed{Sector: "insurance", Name: "Meridian"}

// gatedPairs returns what ui/export.checkLegible hands the gate: the pairs a
// reader is shown on a theme's own surfaces, and the pairs whose background the
// role layer derives by mixing a foreground into a surface.
func gatedPairs() []design.RolePair {
	return append(style.BodyRolePairs(), style.TintedRolePairs()...)
}

func TestClientResolveRefusesAnOverrideTheExportGateWouldRefuse(t *testing.T) {
	cases := []struct{ theme, token, value string }{
		// A warm neutral card surface, the kind of override a brand team writes.
		{"dark", "surface-primary", "#2e2920"},
		// The accent itself, held to 4.5:1 on all three surfaces by the token
		// gate, measured on the tint the layer mixes out of it.
		{"dark", "accent-default", "#f46218"},
		// text-primary, whose role form fg-secondary is in BodyRolePairs — so
		// the gap is not a property of the tinted pair alone.
		{"dark", "text-primary", "#fa19fa"},
	}
	pairs := gatedPairs()
	accepted, refused, failing := 0, 0, 0
	for _, c := range cases {
		client := design.Client{
			Slug:   "meridian",
			Seed:   meridianSeed,
			Tokens: map[string]map[string]string{c.theme: {c.token: c.value}},
		}
		// Reachability, without the defect's own output: the seed on its own is
		// a pair both halves accept, and the literal is an override the client
		// validator admits. If either stops being true the case is not measuring
		// anything, and saying so is better than passing quietly.
		clean, err := design.FromSeed(meridianSeed)
		if err != nil {
			t.Fatalf("FromSeed(%v): %v", meridianSeed, err)
		}
		if err := clean.CheckRoles(style.RoleColors(), pairs); err != nil {
			t.Fatalf("the case measures nothing: the unoverridden seed already fails: %v", err)
		}
		if err := client.Validate(); err != nil {
			t.Fatalf("the case measures nothing: %s.%s=%s is not a legal override: %v", c.theme, c.token, c.value, err)
		}
		pair, err := client.Resolve()
		if err != nil {
			refused++
			t.Logf("%s.%s=%s refused by Client.Resolve: %v", c.theme, c.token, c.value, err)
			continue
		}
		accepted++
		theme := pair.Dark
		if c.theme == "light" {
			theme = pair.Light
		}
		if err := theme.CheckRoles(style.RoleColors(), pairs); err != nil {
			failing++
			t.Errorf("Client.Resolve accepted %s.%s=%s and the pair the export gates is under the floor: %v",
				c.theme, c.token, c.value, err)
		}
	}
	t.Logf("%d overrides accepted, %d refused by Client.Resolve, %d of the accepted ones under the gated floor",
		accepted, refused, failing)
}

// TestClientResolveAndTheExportGateAgreeAboutAnUnoverwrittenPair is the pin. The
// two halves of the gate do agree about every palette the generator produces and
// about the palette this repository ships — that is what makes the disagreement
// above a gap at one seam rather than a broken ruler — and nothing else in the
// repository says so at the seam a client's file arrives at.
func TestClientResolveAndTheExportGateAgreeAboutAnUnoverriddenPair(t *testing.T) {
	pairs := gatedPairs()
	if len(pairs) == 0 {
		t.Fatal("the role layer names no gated pair: nothing here would be measured")
	}
	check := func(label string, pair design.Pair) {
		t.Helper()
		if err := pair.Check(); err != nil {
			t.Errorf("%s: the token half refuses a pair this repository generates: %v", label, err)
		}
		for _, theme := range pair.Both() {
			if err := theme.CheckRoles(style.RoleColors(), pairs); err != nil {
				t.Errorf("%s (%s): the role half refuses a pair the token half accepted: %v", label, theme.Name, err)
			}
		}
	}
	check("design.Default()", design.Default())
	for _, seed := range []design.Seed{
		{Sector: "insurance", Name: "Meridian"},
		{Sector: "utilities", Name: "Harbour Light"},
		{Sector: "logistics", Name: "Northline", Brand: "#1f6f8b"},
		{Sector: "retail", Name: "Kestrel & Co"},
	} {
		client := design.Client{Slug: "meridian", Seed: seed}
		pair, err := client.Resolve()
		if err != nil {
			t.Errorf("Client.Resolve(%+v): %v", seed, err)
			continue
		}
		check(seed.Name, pair)
	}
}
