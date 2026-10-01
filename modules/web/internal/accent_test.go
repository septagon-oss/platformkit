package internal

// The tenant's brand colour is one value and the sheet paints it in two modes.
// These cases hold the declarations view writes: one per mode the page can be in,
// each the colour the palette it links can carry in that mode, and none at all
// when the palette cannot answer.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
)

// paintedAccent finds one declaration of the tenant's token with the selector it
// was written under: the plain :root, a mode the visitor stored, or the browser's
// own preference in the sheet's :root:not([data-theme]) form.
var paintedAccent = regexp.MustCompile(`(:root|\[data-theme="(light|dark)"\]|@media \(prefers-color-scheme: dark\)\{:root:not\(\[data-theme\]\))\{--pk-color-accent-default:(#[0-9a-f]{6})\}`)

// accents is the head read as the browser reads it: the colour each mode paints.
// The :root declaration belongs to the mode the page is pinned to — "pinned" —
// and a mode declared twice has to be declared the same both times, because the
// stored-mode rule and the preference rule are the same answer reached two ways.
func accents(t *testing.T, head, pinned string) map[string]string {
	t.Helper()
	out := map[string]string{}
	put := func(mode, hex string) {
		if seen, ok := out[mode]; ok && seen != hex {
			t.Fatalf("mode %s is declared as both %s and %s in %s: the later rule decides it in the browser", mode, seen, hex, head)
		}
		out[mode] = hex
	}
	for _, match := range paintedAccent.FindAllStringSubmatch(head, -1) {
		mode := match[2]
		switch {
		case match[2] == "" && strings.HasPrefix(match[1], "@media"):
			mode = "dark"
		case match[2] == "":
			mode = pinned
		}
		put(mode, match[3])
	}
	if len(out) == 0 {
		t.Fatalf("no accent is declared in %s", head)
	}
	return out
}

// pairPainting is the installation's palette with one theme's accent replaced,
// which is what an unlayered :root declaration does to the tokens layer in a
// browser. The other theme travels as it stands: the page paints one mode.
func pairPainting(theme, hex string) design.Pair {
	pair := design.Default()
	if theme == "dark" {
		pair.Dark.AccentDefault = hex
	} else {
		pair.Light.AccentDefault = hex
	}
	return pair
}

// TestTheSiteDeclaresTheColourEachModeCanCarry: a tenant that names no theme gets
// the page in the visitor's own scheme, which may be either, so both modes have to
// be answered. One colour reading in both would be the interesting claim, and the
// one that is false for a white: it is a link nobody sees on a light card or a
// label nobody sees on a filled button, and it is the mode the sheet has to answer
// separately rather than the tenant that has to be told.
func TestTheSiteDeclaresTheColourEachModeCanCarry(t *testing.T) {
	t.Parallel()
	site := Site{Theme: design.Default()}
	head := render(t, site.view(&sitecontracts.SiteSettings{Theme: "system", PrimaryColor: "#ffffff"},
		page.Request{}, "Welcome", nil).Head)
	painted := accents(t, head, "light")
	for _, mode := range []string{"light", "dark"} {
		hex, ok := painted[mode]
		if !ok {
			t.Fatalf("mode %s carries no declaration in %s, so a visitor in that scheme paints the colour the other mode was measured for", mode, head)
		}
		pair := pairPainting(mode, hex)
		if err := pair.Check(); err != nil {
			t.Errorf("mode %s paints %s: %v", mode, hex, err)
		}
		if err := pair.CheckRoles(design.RoleLayer(), design.GatedRolePairs()); err != nil {
			t.Errorf("mode %s paints %s: %v", mode, hex, err)
		}
	}
	if painted["light"] == painted["dark"] {
		t.Errorf("both modes were issued %s, which reads in neither: the two schemes have different surfaces and one colour cannot read on both", painted["light"])
	}
	if !strings.Contains(head, "@media (prefers-color-scheme: dark)") {
		t.Errorf("a site that named no theme declared no preference-following rule, so a dark-mode visitor with no stored choice would get the light page's colour: %s", head)
	}
}

// TestTheSiteKeepsTheTenantColourThatReads: the repair is for the colour that
// cannot be painted, not for every colour a tenant owns. A brand the gate issues
// reaches the page as the bytes that were typed — apart from their case — and the
// mode the page is pinned to is declared once, under :root.
func TestTheSiteKeepsTheTenantColourThatReads(t *testing.T) {
	t.Parallel()
	site := Site{Theme: design.Default()}
	head := render(t, site.view(&sitecontracts.SiteSettings{Theme: "dark", PrimaryColor: "#C0FFEE"},
		page.Request{}, "Welcome", nil).Head)
	if !strings.Contains(head, ":root{--pk-color-accent-default:#c0ffee}") {
		t.Errorf("a dark theme's mint reads and was moved anyway: %s", head)
	}
	if strings.Contains(head, `[data-theme="dark"]`) {
		t.Errorf("the pinned mode was declared a second time under its own selector, where it would outrank the :root this case just read: %s", head)
	}
	if got := accents(t, head, "dark")["light"]; got == "" || got == "#c0ffee" {
		t.Errorf("the light mode of a dark-pinned site was issued %q: a visitor who stored light gets this site too, and %s is what the light surfaces cannot carry", got, got)
	}
}

// TestTheSiteDeclaresNoAccentItsPaletteCannotCarry: an illegible answer is not
// painted. A composition that hands the site a palette with no colours in it has
// no surfaces to measure against, and that page keeps the accent its stylesheet
// carries — the palette's own, legible by that sheet's own gate — rather than a
// colour nobody measured. The other half of the same palette still reads, so it
// still paints its tenant's colour: the refusal is per mode, not per page.
func TestTheSiteDeclaresNoAccentItsPaletteCannotCarry(t *testing.T) {
	t.Parallel()
	site := Site{Theme: design.Pair{Light: design.Theme{}, Dark: design.Default().Dark}}
	if v := site.view(&sitecontracts.SiteSettings{Theme: "light", PrimaryColor: "#c0ffee"}, page.Request{}, "Welcome", nil); len(v.Head) != 0 {
		t.Errorf("an empty light palette answered for the light mode: head = %s", render(t, v.Head))
	}
	if v := site.view(&sitecontracts.SiteSettings{Theme: "dark", PrimaryColor: "#c0ffee"}, page.Request{}, "Welcome", nil); !strings.Contains(render(t, v.Head), "#c0ffee") {
		t.Error("the dark half of the palette reads and was refused its tenant's colour too")
	}
}
