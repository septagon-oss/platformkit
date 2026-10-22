package internal

// Review round 21 (T-0107).
//
// The measurements below are of the tree this case was written against, where
// DefaultPrimaryColor was #2563eb; that colour has since moved to the one the gate
// issues for it, which is the cure this case asked for.
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
	// The second claim the reviewer measured — that design.Default().LegibleAccent
	// answers #2563eb in both modes, "the same claim from the door below the paint"
	// — is not asserted here. Root ruled it retired on 2026-10-11; the arithmetic it
	// asks for does not exist. The gate holds accent-default on each theme's own
	// canvas at MinContrast 4.5, which needs the two canvases to be 4.5 × 4.5 =
	// 20.25:1 apart, and design.Default() puts #f2efe7 (L 0.8638) over #0e1614
	// (L 0.0072) at 15.98:1 — a ground that dark needs the other at L 1.1078, past
	// the 1.0 a colour can reach. All 16,777,216 #rrggbb values were measured: none
	// clears the gate in both modes. Only "" answers both, and a row cannot hold it
	// while the published wire pattern is ^#[0-9a-fA-F]{6}$ (rule B3); that widening
	// is T-0503. What this case can assert, and does, is the half the system can be
	// consistent about: the colour the contract names is the colour the paint emits.
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
