package internal

// Review round 21 (T-0107).
//
// Round 19 found the tenant's brand colour painted ungated; `ede5f4a` cured the
// paint by measuring it through `design.Pair.LegibleAccent` before it is written
// into the head. That cure has a consequence nobody has looked at, and it is not
// in the cured path's own tests, which all start from a colour a *person* typed:
// the colour a site that chose none is rendered in.
//
//	modules/site/contracts/site.go:40  // DefaultPrimaryColor is the colour a site that has chosen none is rendered in.
//	modules/site/contracts/site.go:43  const DefaultPrimaryColor = "#2563eb"
//	                                   … the same value as the column default:  gorm:"…default:'#2563eb'"
//
// Measured (both halves, this round):
//
//	$ go run .   # /tmp/pkit-r21/probe, design.Default()
//	light asked #2563eb painted #4164b0 changed=true
//	dark  asked #2563eb painted #618ef1 changed=true
//	light with accent #2563eb: Check=light: accent-default on surface-canvas measures 4.50:1, below 4.5:1
//
// So the contract's own default is a colour this repository's gate refuses — by
// four thousandths of a ratio — and the cure does exactly what it was written to
// do with it: it paints another colour. Served on this round's own stack, with a
// `site_settings` row left at the schema default:
//
//	$ curl -s -H "Host: r21-site.localhost" http://127.0.0.1:18121/   # origin/main, before the cure
//	  <style>:root{--pk-color-accent-default:#2563eb}</style>
//	$ curl -s -H "Host: r21-site.localhost" http://127.0.0.1:18122/   # this head
//	  <style>:root{--pk-color-accent-default:#4164b0}[data-theme="dark"]{--pk-color-accent-default:#618ef1}…
//
// The paint is right and the comment is right; the *default* is now the thing
// that is wrong. Every tenant that never opened the settings screen — which is
// every tenant until somebody opens it — is rendered in a colour that is not the
// one its row holds, not the one the contract names, and not the one the settings
// form shows, and `SiteSettings.Validate` (modules/site/contracts/site.go:165)
// checks only the `#rrggbb` grammar, so the person who opens the form and saves
// what it shows is told nothing and keeps the same mismatch. That is a colour
// decision (move the default, or show the measured colour where the stored one is
// shown), not a legibility failure, which is why it is not filed as a HIGH here.
//
// The case below asserts the self-consistency the system claims for itself: a site
// that chose no colour paints the colour its own contract says such a site is
// rendered in. It fails at these bytes and goes green the day the default is a
// colour the gate would issue — no other cure can make it pass, which is the point.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
)

// TestASiteThatChoseNoColourPaintsTheColourTheContractNamesForOne is the finding.
func TestASiteThatChoseNoColourPaintsTheColourTheContractNamesForOne(t *testing.T) {
	settings := &sitecontracts.SiteSettings{Title: "No colour chosen"}
	settings.PrimaryColor = sitecontracts.DefaultPrimaryColor
	for _, mode := range []string{"system", "light", "dark"} {
		settings.Theme = mode
		rules := Site{}.accent(settings)
		if !strings.Contains(rules, sitecontracts.DefaultPrimaryColor) {
			t.Errorf("mode %s: a site that named no colour paints %q, which is not the %s the contract names as the colour such a site is rendered in; the gate moved it, so the row, the settings form and the page disagree about the tenant's colour",
				mode, rules, sitecontracts.DefaultPrimaryColor)
		}
	}
	// The same claim from the door below the paint, so the finding cannot be
	// dismissed as a rendering choice: the contract's default does not clear the
	// gate the paint now measures with.
	for _, mode := range []string{"light", "dark"} {
		got, err := design.Default().LegibleAccent(mode, sitecontracts.DefaultPrimaryColor)
		if err != nil {
			t.Errorf("%s: LegibleAccent refuses the contract's own default: %v", mode, err)
			continue
		}
		if got != sitecontracts.DefaultPrimaryColor {
			t.Errorf("%s: the contract says a site that chose none is rendered in %s and the gate repaints it as %s; a default the paint refuses is a default that is never painted",
				mode, sitecontracts.DefaultPrimaryColor, got)
		}
	}
}

// TestAChosenColourThatReadsIsPaintedUnchanged is the guard the finding must not
// be read as: the cure is not a repaint of everything. A colour that clears the
// gate keeps all of itself, so the mismatch above is about the default alone.
func TestAChosenColourThatReadsIsPaintedUnchanged(t *testing.T) {
	const chosen = "#0f5d4e" // the palette's own light accent
	got, err := design.Default().LegibleAccent("light", chosen)
	if err != nil {
		t.Fatalf("LegibleAccent(%s): %v", chosen, err)
	}
	if got != chosen {
		t.Errorf("a brand colour that clears the gate was moved to %s; the cure is meant to move only what does not read", got)
	}
}
