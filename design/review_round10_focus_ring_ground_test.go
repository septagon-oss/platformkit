package design

import "testing"

// Review round 10. The focus ring is the one body-role graphic this package names
// a floor for — MinContrastGraphic is WCAG 2.2 SC 1.4.11, and bodyContrast reads
// the ring against "the page". It reads it against one ground of the three the
// kernel paints it on: clFocusRing (ui/components/classlists.go:83) draws a 2px
// ring in --pk-role-ring-focus with a 2px *offset*, so the ring lands on whatever
// surface the focused control sits inside — clCardFrame's surface-primary
// (classlists.go:50,60) or the muted surface a panel raises onto — and no pair in
// bodyContrast or GatedRolePairs names either of those grounds.
//
// The case below takes a ring that sits exactly on the measured floor, 3.000:1 on
// the canvas, and asks what it measures where it is drawn. It reaches its
// assertion through the status Client.Resolve returns — refused, accepted — never
// through a refusal's wording, and its second case pins the acceptance the cure
// must preserve, so a fix that refused every ring override could not pass it.

// ringGrounds measures this theme's ring against the three surfaces a focused
// control can sit on. The ring colour is read back out of the finished theme, so
// the case measures what shipped rather than what it typed.
func ringGrounds(t *testing.T, theme Theme) map[string]float64 {
	t.Helper()
	values, err := theme.colorValues()
	if err != nil {
		t.Fatalf("resolve %s theme: %v", theme.Name, err)
	}
	ring, ok := values["--pk-color-focus"]
	if !ok {
		t.Fatalf("%s theme exports no --pk-color-focus", theme.Name)
	}
	out := map[string]float64{}
	for _, ground := range []string{"surface-canvas", "surface-primary", "surface-muted"} {
		background, ok := values["--pk-color-"+ground]
		if !ok {
			t.Fatalf("%s theme exports no --pk-color-%s", theme.Name, ground)
		}
		out[ground] = Contrast(ring, background)
	}
	return out
}

// A client may override the ring, and must be refused when the ring it overrides
// it to disappears on the surface the kernel draws it on.
func TestClientResolveRefusesARingThatVanishesOnTheGroundItIsDrawnOn(t *testing.T) {
	client := Client{
		Slug: "roundten",
		Seed: Seed{Sector: "health", Name: "Focus Probe"},
		Tokens: map[string]map[string]string{
			"dark": {"focus": "#905546"},
		},
	}
	pair, err := client.Resolve()
	if err == nil {
		// Accepted: measure what the accepted pair paints, and fail on the number.
		for _, theme := range pair.Both() {
			grounds := ringGrounds(t, theme)
			for _, ground := range []string{"surface-primary", "surface-muted"} {
				if grounds[ground] < MinContrastGraphic {
					t.Errorf("client %s was accepted, but its ring on %s (%s) measures %.3f:1, below the %.1f:1 %s the package declares: name the ground in the gate",
						client.Slug, ground, theme.Name, grounds[ground], MinContrastGraphic, "SC 1.4.11")
				}
			}
		}
		return
	}
	// Refused, as the gate should refuse it. The refusal must still be about this
	// ring and not about some other property of the same file.
	if _, seedErr := FromSeed(client.Seed); seedErr != nil {
		t.Fatalf("the seed alone was refused (%v), so this case proves nothing about the ring", seedErr)
	}
}

// The same door keeps accepting a client whose ring clears every ground it is
// drawn on. This is the half the cure must not break: it is why the answer is to
// name the two missing grounds, not to distrust ring overrides.
func TestClientResolveAcceptsARingThatClearsEveryGroundItIsDrawnOn(t *testing.T) {
	pair, err := Client{
		Slug: "roundtengood",
		Seed: Seed{Sector: "health", Name: "Focus Probe"},
		Tokens: map[string]map[string]string{
			"dark": {"focus": "#c15bb0"},
		},
	}.Resolve()
	if err != nil {
		t.Fatalf("a ring that clears every ground was refused: %v", err)
	}
	for _, theme := range pair.Both() {
		for ground, got := range ringGrounds(t, theme) {
			if got < MinContrastGraphic {
				t.Errorf("accepted client's ring on %s (%s) measures %.3f:1, below %.1f:1",
					ground, theme.Name, got, MinContrastGraphic)
			}
		}
	}
}

// The shipped palette and the generated family are the two sets the cure must not
// refuse: this case is the measurement that says adding the two grounds costs
// nothing at either tree.
func TestShippedAndGeneratedRingsClearEveryGroundTheyAreDrawnOn(t *testing.T) {
	pairs := map[string]Pair{"design.Default()": Default()}
	for _, sector := range []string{"health", "legal", "finance", "retail", "design", "public"} {
		for i := range 40 {
			name := sector + string(rune('A'+i%26)) + string(rune('a'+i/26))
			pair, err := FromSeed(Seed{Sector: sector, Name: name})
			if err != nil {
				t.Fatalf("seed %s/%s was refused: %v", sector, name, err)
			}
			pairs[sector+"/"+name] = pair
		}
	}
	for slug, pair := range pairs {
		for _, theme := range pair.Both() {
			for ground, got := range ringGrounds(t, theme) {
				if got < MinContrastGraphic {
					t.Errorf("%s (%s): the ring on %s measures %.3f:1, below the %.1f:1 a gate that measured this ground would demand",
						slug, theme.Name, ground, got, MinContrastGraphic)
				}
			}
		}
	}
}
