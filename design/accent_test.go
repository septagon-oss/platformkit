package design

import (
	"strings"
	"testing"
)

// The colour a row holds is the one input to a palette that no file, no seed and
// no generator passes through. These cases hold the door that measures it: the
// colour a pair already wears comes back as it stands, a colour that would paint
// an unreadable page comes back legible, and what names no colour is refused
// rather than measured as one.

// TestLegibleAccentLeavesTheColoursTheGateIssuesAlone: the accent each theme of
// the shipped pair wears is a colour the gate issues, so asking for it has to
// return it untouched — a repair that moved the installation's own brand colour
// would repaint every site that never chose one.
func TestLegibleAccentLeavesTheColoursTheGateIssuesAlone(t *testing.T) {
	t.Parallel()
	pair := Default()
	for _, theme := range []string{"light", "dark"} {
		want := pair.Light.AccentDefault
		if theme == "dark" {
			want = pair.Dark.AccentDefault
		}
		got, err := pair.LegibleAccent(theme, strings.ToUpper(want))
		if err != nil {
			t.Fatalf("%s accent %s: %v", theme, want, err)
		}
		if got != strings.ToLower(want) {
			t.Errorf("%s: LegibleAccent(%s) = %s, the same colour lower-case", theme, want, got)
		}
	}
}

// TestLegibleAccentRepairsWhatTheTokenGateWouldRefuse is the property: whatever
// colour is asked for, the pair that comes back is one the gate issues, or the
// answer is an error. The colours are the ones a tenant can type and a browser
// would paint — the column's default, white, black, a colour the colour of the
// page itself — chosen for what they measure rather than to flatter the walk.
func TestLegibleAccentRepairsWhatTheTokenGateWouldRefuse(t *testing.T) {
	t.Parallel()
	pair := Default()
	for _, theme := range []string{"light", "dark"} {
		for _, value := range []string{
			"#2563eb", "#c0ffee", "#f7f7f2", "#ffffff", "#000000", "#0f5d4e", "#ffd23f",
		} {
			got, err := pair.LegibleAccent(theme, value)
			if err != nil {
				t.Errorf("%s/%s: %v", theme, value, err)
				continue
			}
			probed := themeNamed(pair, theme).withAccent(got)
			if !probed.gated() {
				t.Errorf("%s/%s repaired to %s, which the gate still refuses: %v / %v",
					theme, value, got, probed.Check(), probed.CheckRoles(RoleLayer(), GatedRolePairs()))
			}
			again, err := pair.LegibleAccent(theme, got)
			if err != nil || again != got {
				t.Errorf("%s/%s: repairing %s again gave %q, %v — the walk is not at rest at its own answer",
					theme, value, got, again, err)
			}
		}
	}
}

// TestLegibleAccentRefusesWhatNamesNoColourOrNoTheme: the refusals a caller acts
// on, each with no colour beside it. A theme outside light and dark names no
// surfaces; a value this package cannot read, or one that carries alpha, names no
// ratio — Client.Validate refuses an override for that reason and Theme.Check
// refuses to measure it, so the door that paints may not either.
func TestLegibleAccentRefusesWhatNamesNoColourOrNoTheme(t *testing.T) {
	t.Parallel()
	pair := Default()
	for _, theme := range []string{"sepia", "", "system", "Light"} {
		got, err := pair.LegibleAccent(theme, "#0f5d4e")
		if err == nil || got != "" {
			t.Errorf("theme %q returned %q, %v", theme, got, err)
		}
	}
	for _, value := range []string{"red", "#0f5d4e80", "rgba(0,0,0,1)", "", "#0f5d4e;"} {
		got, err := pair.LegibleAccent("light", value)
		if err == nil || got != "" {
			t.Errorf("value %q returned %q, %v", value, got, err)
		}
	}
	// Shorthand is a colour this package reads and the gate can measure, so it is
	// answered rather than refused: the strict six-digit grammar is the caller's
	// (the site module guards the style element it interpolates the value into),
	// and what comes back here is always the literal a theme stores.
	if got, err := pair.LegibleAccent("light", "#fff"); err != nil || got == "#fff" || !pair.Light.withAccent(got).gated() {
		t.Errorf("#fff returned %q, %v", got, err)
	}
	// The refusal is per theme, not per pair: a palette broken in the mode nobody
	// is painting is asked nothing about the one that reads. The pair whose
	// *painted* theme names no colours is the one with no surfaces to search, and
	// it says so instead of answering with a colour nobody measured.
	if got, err := (Pair{Light: Default().Light, Dark: Theme{}}).LegibleAccent("light", "#0f5d4e"); err != nil || got == "" {
		t.Errorf("a broken dark half refused the light half, which reads on its own: %q, %v", got, err)
	}
	if got, err := (Pair{Light: Theme{}, Dark: Default().Dark}).LegibleAccent("light", "#0f5d4e"); err == nil || got != "" {
		t.Errorf("an empty light theme returned %q, %v", got, err)
	}
}

// themeNamed is the half of a pair a caller named.
func themeNamed(pair Pair, name string) Theme {
	if name == "dark" {
		return pair.Dark
	}
	return pair.Light
}

// TestLegibleAccentKeepsTheHueItWasGiven is the most of a brand the walk is
// allowed to lose: it moves toward the pole that reads and gives up saturation on
// the way, and it never swings to another hue. A cure that answered every
// unreadable brand colour with black would pass every ratio in this package and
// be worth nothing to the tenant who typed a green.
func TestLegibleAccentKeepsTheHueItWasGiven(t *testing.T) {
	t.Parallel()
	pair := Default()
	for _, theme := range []string{"light", "dark"} {
		for _, value := range []string{"#0f5d4e", "#2563eb", "#c0ffee", "#7b2fbf"} {
			got, err := pair.LegibleAccent(theme, value)
			if err != nil {
				t.Fatalf("%s/%s: %v", theme, value, err)
			}
			wantHue, wantSat, _ := rgbToHSV(mustParse(value))
			hue, sat, _ := rgbToHSV(mustParse(got))
			if sat >= wantSat+0.01 {
				t.Errorf("%s/%s came back as saturated as it went in (%.2f): the walk trades saturation, not hue", theme, value, sat)
			}
			if wantSat > 0.2 && sat > 0.05 && hueDiff(hue, wantHue) > 3 {
				t.Errorf("%s/%s came back at hue %.0f rather than %.0f", theme, value, hue, wantHue)
			}
		}
	}
}

func hueDiff(a, b float64) float64 {
	d := a - b
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	if d < 0 {
		return -d
	}
	return d
}
