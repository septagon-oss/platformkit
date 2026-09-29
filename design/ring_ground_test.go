package design

import (
	"strings"
	"testing"
)

// The focus ring is drawn outside the control it rings, so it is painted on the
// surface the control sits on rather than on the page behind it. These cases hold
// the three properties the cure for that depends on: the token gate names each of
// the three surfaces the offset can land on, the role layer needs no fourth list
// because its ring pair *is* one of these measurements, and one ground can fail
// while the other two hold, so a refusal has to name the one it measured.

// TestRingGateNamesEveryGroundTheOffsetPutsItOn pins the list entry by entry: a
// gate that measured the ring against one surface would say so here rather than in
// a reviewer's sweep.
func TestRingGateNamesEveryGroundTheOffsetPutsItOn(t *testing.T) {
	gated := map[string]float64{}
	for _, pair := range bodyContrast {
		if pair.foreground == "focus" {
			gated[pair.background] = pair.min
		}
	}
	for _, ground := range []string{"surface-canvas", "surface-primary", "surface-muted"} {
		min, ok := gated[ground]
		if !ok {
			t.Errorf("bodyContrast gates the focus ring on no %s, where clFocusRing's RingOffset(RingOffset2) draws it", ground)
			continue
		}
		if min != MinContrastGraphic {
			t.Errorf("bodyContrast gates focus on %s at %v, not the graphic's floor %v", ground, min, MinContrastGraphic)
		}
	}
	if len(gated) != 3 {
		t.Errorf("bodyContrast gates the ring on %d grounds, want the three surfaces a theme has", len(gated))
	}
}

// TestRingRoleIsOneOfTheseMeasurements is why the ring needs no entry in the role
// pair lists: ring-focus is a bare reference to the focus token and the three
// surface roles are bare references to the three surface tokens, so measuring the
// role layer re-measures these pairs to the last digit rather than adding a pair.
// A round that added a ring role pair would double the list and change no number;
// a round that later mixed a ring role would fail here, which is the case that
// would need the list.
func TestRingRoleIsOneOfTheseMeasurements(t *testing.T) {
	roles := RoleLayer()
	for name, pair := range map[string]Pair{"design.Default()": Default(), "client": mustRingClient(t)} {
		for _, theme := range pair.Both() {
			tokens, err := ResolveColors(theme.Tokens(), nil)
			if err != nil {
				t.Fatalf("%s/%s: resolve tokens: %v", name, theme.Name, err)
			}
			layer, err := ResolveColors(theme.Tokens(), roles)
			if err != nil {
				t.Fatalf("%s/%s: resolve roles: %v", name, theme.Name, err)
			}
			for _, ground := range []struct{ role, token string }{
				{"surface-primary", "surface-primary"},
				{"surface-secondary", "surface-canvas"},
				{"surface-tertiary", "surface-muted"},
			} {
				role := Contrast(layer["--pk-role-ring-focus"], layer["--pk-role-"+ground.role])
				token := Contrast(tokens["--pk-color-focus"], tokens["--pk-color-"+ground.token])
				if role != token {
					t.Errorf("%s/%s: ring-focus on %s measures %0.6f:1 in the role layer, focus on %s measures %0.6f:1 in the token layer",
						name, theme.Name, ground.role, role, ground.token, token)
				}
			}
		}
	}
}

// TestClientResolveRefusesTheRingOnTheGroundAloneThatFails takes a ring that, over
// the surfaces this client's own seed generates, holds on the canvas at 3.748:1
// and on the card at 3.359:1 and falls to 2.871:1 on the muted surface, and asks
// that the door refuse it and name the ground it measured. The gate before this
// round read only the canvas and accepted the ring — which is why the refusal, and
// not only the ratio, has to say which surface it stood on.
func TestClientResolveRefusesTheRingOnTheGroundAloneThatFails(t *testing.T) {
	_, err := Client{
		Slug:   "ringmuted",
		Seed:   Seed{Sector: "health", Name: "Ring Ground"},
		Tokens: map[string]map[string]string{"dark": {"focus": "#006cf6"}},
	}.Resolve()
	if err == nil {
		t.Fatal("a ring at 3.748:1 on the canvas, 3.359:1 on the card and 2.871:1 on the muted surface was accepted")
	}
	for _, want := range []string{"focus", "surface-muted", "2.87:1", "3.0:1"} {
		// Check's own doc promises the pair it measured and the ratio it got: both
		// token names, the ratio and the floor. This reads those properties, not the
		// sentence around them.
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not carry the %q Check promises to name", err, want)
		}
	}
}

// TestShippedRingHoldsOnTheSidebarGroundTheGateDoesNotName measures the fourth
// surface a ringed control sits on — clSidebarBrandLink and the sidebar's own
// disclosure buttons merge clFocusRing, and their ground is sidebar-bg — and holds
// the shipped palette above the graphic floor there. It is a pin, not a gate:
// naming this ground in bodyContrast would refuse 1762 of the 8000 themes the
// generator emits over 4000 seeds (worst 2.408:1, health/Client25 light), so the
// ring would have to be repaired toward the sidebar the way a tint is repaired
// toward the copy on it. That repair is a colour decision and stays with the other
// grounds named in the follow-up brief; what this case refuses is silence about the
// number while the shipped ring clears it at 3.57:1 and 7.62:1.
func TestShippedRingHoldsOnTheSidebarGroundTheGateDoesNotName(t *testing.T) {
	for _, theme := range Default().Both() {
		values, err := ResolveColors(theme.Tokens(), nil)
		if err != nil {
			t.Fatalf("resolve %s tokens: %v", theme.Name, err)
		}
		if got := Contrast(values["--pk-color-focus"], values["--pk-color-sidebar-bg"]); got < MinContrastGraphic {
			t.Errorf("%s: the ring on sidebar-bg measures %.3f:1, below the %.1f:1 floor it is gated at on the three body surfaces",
				theme.Name, got, MinContrastGraphic)
		}
	}
}

// mustRingClient returns a client pair whose ring was named, so the identity case
// measures a palette carrying someone else's ring and not only the shipped one.
// The override it carries measures 6.261:1 on the canvas, 7.260:1 on the card and
// 5.470:1 on the muted surface, so it is accepted by the gate as it stands.
func mustRingClient(t *testing.T) Pair {
	t.Helper()
	pair, err := Client{
		Slug:   "ringidentity",
		Seed:   Seed{Sector: "retail", Name: "Ring Identity"},
		Tokens: map[string]map[string]string{"light": {"focus": "#8a2f74"}},
	}.Resolve()
	if err != nil {
		t.Fatalf("a ring that clears every ground was refused: %v", err)
	}
	return pair
}
