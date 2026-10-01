package internal

// Review round 20 (T-0107). The tenant's accent is measured now, and this round
// re-ran the measurement over 8 192 tenant colours in both themes: every colour
// design.Pair.LegibleAccent returned cleared both halves of the gate. The ratio
// arithmetic is sound. What no case here holds is *which palette* it is sound for.
//
// The three accent cases the delivery ships (accent_test.go) and the case review
// round 19 left all answer that question the same way: they render a Site and then
// measure the colour it painted against design.Default() — pairPainting and
// pairWithAccent both start from design.Default(). The code under test measures
// against s.palette(). Those two are equal only while the site is composed with the
// default pair, which is what apps/platformkit does today. Hand the same Site a
// palette of its own — the thing design.Client.Resolve exists to produce, and the
// reason Site.Theme is a field at all — and every one of those cases would still
// pass if accent() measured the installation's pair while the page linked the
// composed one: the colour would be legal for a sheet nobody serves and illegal for
// the one everybody reads. mount.go states the claim the cases do not hold ("one
// pair, so the measurement is of the sheet the page links rather than of a palette
// nobody renders"); this case holds it.
//
// The second assertion is one no case reads either. An unlayered :root and an
// unlayered [data-theme="dark"] carry the same specificity — one class-rank simple
// selector each — so the mode selector wins by source order alone. Move it before
// the :root and the tenant's light answer beats the tenant's dark answer for every
// visitor who stored a mode, with nothing failing but the page. So the order is
// asserted, per declaration, through the bytes the module writes.
//
// Both halves pass at these bytes. Nothing here prescribes a fix: it holds the
// relationship between the pair the sheet is made of and the pair the colour is
// measured against, which is what makes the repair mean anything.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
)

// everyAccent reads each declaration of the tenant's token out of a head, with the
// selector it was written under, so a case can ask both what colour and where.
var everyAccent = regexp.MustCompile(`([^{}\s;>]+)\{--pk-color-accent-default:(#[0-9a-fA-F]{6})\}`)

type accentRule struct {
	selector string
	hex      string
	at       int
	// mode is the theme this declaration governs in a browser: :root carries the
	// mode the page is pinned to, a [data-theme] rule carries the stored choice,
	// and the sheet's :root:not([data-theme]) form inside a dark media query
	// carries the browser's own preference.
	mode string
}

func accentRules(t *testing.T, head, pinned string) []accentRule {
	t.Helper()
	var out []accentRule
	for _, match := range everyAccent.FindAllStringSubmatchIndex(head, -1) {
		selector := head[match[2]:match[3]]
		mode := pinned
		switch {
		case strings.HasPrefix(selector, "[data-theme="):
			mode = strings.Trim(selector[len("[data-theme="):len(selector)-1], `"`)
		case strings.HasPrefix(selector, ":root:not("):
			// The media block's inner selector: this only reaches a document with
			// no stored choice, and only under the sheet's own dark query.
			mode = "dark"
		}
		out = append(out, accentRule{selector: selector, hex: head[match[4]:match[5]], at: match[0], mode: mode})
	}
	if len(out) == 0 {
		t.Fatalf("the head declares no %s, so this case measures no paint: %s", "--pk-color-accent-default", head)
	}
	return out
}

// composedPalette is a client's own pair: what FromSeed issues for a seed this
// repository's corpus does not hold. It stands for a composition that did not take
// the installation's colours, which is the state every other case in this package
// is blind to. A palette no door could have produced would make the case measure
// nothing, so it is checked against the gate it has to clear before it is used.
func composedPalette(t *testing.T) design.Pair {
	t.Helper()
	pair, err := design.FromSeed(design.Seed{Sector: "finance", Name: "Meridian Bank", Brand: "#b0413e"})
	if err != nil {
		t.Fatalf("FromSeed refused the seed this case builds its composition from: %v", err)
	}
	if err := pair.Check(); err != nil {
		t.Fatalf("the palette this case composes the site with fails the token gate on its own, so no door could have produced it: %v", err)
	}
	if err := pair.CheckRoles(design.RoleLayer(), design.GatedRolePairs()); err != nil {
		t.Fatalf("the palette this case composes the site with fails the role gate on its own, so no door could have produced it: %v", err)
	}
	if pair == design.Default() {
		t.Fatal("the seed generated the installation's own pair, so this case cannot tell which palette the measurement read")
	}
	return pair
}

// themePainting returns one mode of the pair with its accent replaced, which is
// what an unlayered declaration does to the tokens layer of the sheet the page
// links: the other mode travels as it stands, because this page paints one mode.
func themePainting(pair design.Pair, mode, hex string) design.Theme {
	if mode == "dark" {
		pair.Dark.AccentDefault = hex
		return pair.Dark
	}
	pair.Light.AccentDefault = hex
	return pair.Light
}

// TestTheTenantAccentIsMeasuredAgainstThePaletteThePageLinks holds the claim in
// mount.go's palette(): the colour a page paints is legible for the pair that made
// the stylesheet that page links, for a palette that is not the installation's.
func TestTheTenantAccentIsMeasuredAgainstThePaletteThePageLinks(t *testing.T) {
	t.Parallel()

	site := Site{Theme: composedPalette(t)}
	// A tenant colour per shape of failure: the column's default, a colour that
	// cannot read on a light card, one that cannot read on a dark canvas, and the
	// two poles.
	values := []string{sitecontracts.DefaultPrimaryColor, "#c0ffee", "#ffffff", "#000000", "#f7f7f2"}
	themes := []string{"light", "dark", "system"}
	differing, checked := 0, 0
	for _, theme := range themes {
		for _, hex := range values {
			settings := &sitecontracts.SiteSettings{Theme: theme, PrimaryColor: hex, Title: "Acme"}
			if err := settings.Validate(t.Context()); err != nil {
				t.Fatalf("SiteSettings.Validate refused %q, which this case offers as a colour a tenant can type: %v", hex, err)
			}
			head := render(t, site.view(settings, page.Request{}, "Welcome", nil).Head)
			pinned := "light"
			if theme == "dark" {
				pinned = "dark"
			}
			rules := accentRules(t, head, pinned)
			var rootAt = -1
			for _, r := range rules {
				if r.selector == ":root" {
					rootAt = r.at
					break
				}
			}
			for _, r := range rules {
				checked++
				// The mode's own theme, with the mode's own colour: a pair checked
				// whole would report the mode this page is not painting.
				measured := themePainting(site.palette(), r.mode, r.hex)
				if err := measured.Check(); err != nil {
					t.Errorf("a site composed with its own palette paints %s in mode %s for tenant colour %s (selector %s) and the token gate refuses it: %v",
						r.hex, r.mode, hex, r.selector, err)
				}
				if err := measured.CheckRoles(design.RoleLayer(), design.GatedRolePairs()); err != nil {
					t.Errorf("a site composed with its own palette paints %s in mode %s for tenant colour %s (selector %s) and the role gate refuses it: %v",
						r.hex, r.mode, hex, r.selector, err)
				}
				// Source order, because the two selectors are equally specific and
				// the later one wins in a browser.
				if r.selector != ":root" && rootAt >= 0 && r.at < rootAt {
					t.Errorf("the %s declaration is written before the :root it exists to override; the two carry the same specificity, so a visitor who stored a mode would be painted the other mode's colour: %s",
						r.selector, head)
				}
				// Would the installation's own pair have answered the same? If it
				// would for every colour offered, the assertions above measure one
				// palette and could not tell the two apart.
				if other, err := design.Default().LegibleAccent(r.mode, hex); err == nil && other != r.hex {
					differing++
				}
			}
			// A system page can be in either mode, so both have to be answered —
			// including by the preference-following rule a stored choice cannot
			// rescue.
			if theme == "system" {
				seen := map[string]bool{}
				for _, r := range rules {
					seen[r.mode] = true
				}
				for _, mode := range []string{"light", "dark"} {
					if !seen[mode] {
						t.Errorf("a site that named no theme declared no colour for the %s mode of tenant colour %s: a visitor in that scheme would paint the colour the other mode was measured for", mode, hex)
					}
				}
			}
		}
	}
	if checked < 5 {
		t.Errorf("only %d declarations were read across %d settings, so this case measures a page that mostly declares nothing", checked, len(themes)*len(values))
	}
	if differing == 0 {
		t.Errorf("the composed palette and the installation's issued the same colour for every one of the %d tenant colours offered: this case cannot tell which pair the measurement read, so it pins nothing — give it a palette whose surfaces differ further from the installation's", len(values))
	}
}

// TestTheSiteComposedWithNoPaletteMeasuresWhatItLinks is the other half of the same
// sentence: a site that names no pair paints design.Default(), so the colour it
// paints is legible on design.Default() — the pair every other case in this package
// assumes. That belongs to the module, not to the repository.
func TestTheSiteComposedWithNoPaletteMeasuresWhatItLinks(t *testing.T) {
	t.Parallel()

	site := Site{}
	if got := site.palette(); got != design.Default() {
		t.Fatalf("a site composed with no palette measures a pair other than design.Default(), which is the pair every case in this package measures the answer against: light accent = %s", got.Light.AccentDefault)
	}
	head := render(t, site.view(&sitecontracts.SiteSettings{Theme: "system", PrimaryColor: "#c0ffee"},
		page.Request{}, "Welcome", nil).Head)
	for _, r := range accentRules(t, head, "light") {
		th := themePainting(site.palette(), r.mode, r.hex)
		if err := th.CheckRoles(design.RoleLayer(), design.GatedRolePairs()); err != nil {
			t.Errorf("a site with no palette paints %s in mode %s and the role gate refuses it: %v", r.hex, r.mode, err)
		}
	}
}
