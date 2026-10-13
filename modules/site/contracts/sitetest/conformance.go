// Package sitetest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification written as executable cases; the real service and the fake both
// run it.
package sitetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Fixture is one case's world: a Service, the transaction its operations take,
// the palette that installation is drawn with, and what has been published so far.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Palette is what this case's installation draws itself with, the same value
	// the harness built the Service from. A case names a colour through it rather
	// than writing one out, so the suite says which accent it is pretending and the
	// assertions below stay true when this file's answer changes.
	Palette contracts.Palette
	// Locked is the same service as contracts.LockedReader — the read a caller
	// about to write is handed — so a case can ask whether the two reads answer the
	// same thing. Every implementation here is one; a third party's that is not
	// gets a failure naming the interface rather than a skipped case.
	Locked contracts.LockedReader
	// Published is the events the implementation has published, in order. Half
	// of what Save promises is silence: a screen that submits its form twice
	// must not invalidate a cache twice.
	Published func() []string
}

func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; saving what is already stored changes nothing, so it says nothing", what, after[before:])
	}
}

func (f Fixture) one(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != contracts.EventSettingsUpdated {
		t.Errorf("%s published %v, want [%s]", what, got, contracts.EventSettingsUpdated)
	}
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// InstallationPalette is the accent pair the installation this suite pretends to
// be is drawn with. Both harnesses build their service from it and hand it back as
// Fixture.Palette, so the fake's cases are the SQL service's cases.
//
// Both values are colours the accent rule *refuses* — #0f5d4e reads 6.78:1 against
// the light canvas and 2.36:1 against the dark one, #4bbf9c the other way round —
// and that is the point: the installation's own accent is not a colour a tenant
// could choose, so every case that sends it back is a form echoing a read and not
// a hand typing a colour. A suite whose palette were a storable colour could not
// tell those two apart.
var InstallationPalette = contracts.PaletteFunc(func(mode string) string {
	if mode == contracts.ThemeDark {
		return "#4bbf9c"
	}
	return "#0f5d4e"
})

// acme is a site somebody has configured. Its accent is inside the band the
// contrast rule keeps — #b45309 reads 4.37:1 on the light canvas and 3.66:1 on the
// dark one — because this fixture is the input every accepted case saves, and the
// colour the kit used to ship, #ff8800, is now a refusal input and stays in this
// file as one.
func acme() *contracts.SiteSettings {
	return &contracts.SiteSettings{
		Title: "Acme", Tagline: "We make things", HomeSlug: "welcome",
		Theme: contracts.ThemeDark, PrimaryColor: "#b45309",
		Nav: contracts.Nav{{Label: "About", Path: "/about-us"}, {Label: "Blog", Path: "/blog"}},
	}
}

// save is Save with the error checked, which every case but the refusals does.
func save(t *testing.T, f Fixture, in *contracts.SiteSettings) *contracts.SiteSettings {
	t.Helper()
	out, err := f.Service.Save(f.Ctx, f.Tx, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return out
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"a tenant that has configured nothing has the defaults": func(t *testing.T, f Fixture) {
			f.silent(t, "reading the settings of a site nobody has touched", func() {
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				switch {
				case got.Theme != contracts.ThemeSystem:
					t.Errorf("theme is %q, want %q: a tenant that has said nothing has not chosen dark",
						got.Theme, contracts.ThemeSystem)
				case got.PrimaryColor != f.Palette.Accent(contracts.ThemeLight):
					// The stored colour is DefaultPrimaryColor — nobody chose one — and what
					// the read answers is the installation's own. A tenant that has never
					// opened the form is never handed the empty colour to do with as it likes.
					t.Errorf("the colour is %q, want the installation's %q",
						got.PrimaryColor, f.Palette.Accent(contracts.ThemeLight))
				case got.Title != "" || len(got.Nav) != 0:
					t.Errorf("an unconfigured site says %+v", got)
				}
				if got.Revision != 0 {
					t.Errorf("an unconfigured site is at revision %d; the number counts writes", got.Revision)
				}
			})
		},

		"saving records what was configured": func(t *testing.T, f Fixture) {
			var out *contracts.SiteSettings
			f.one(t, "the first save", func() { out = save(t, f, acme()) })
			if out.ID == uuid.Nil {
				t.Error("the saved settings have no id")
			}
			got, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			switch {
			case got.Title != "Acme" || got.Tagline != "We make things" || got.HomeSlug != "welcome":
				t.Errorf("the settings read back as %+v", got)
			case got.Theme != contracts.ThemeDark || got.PrimaryColor != "#b45309":
				t.Errorf("the look reads back as %q/%q", got.Theme, got.PrimaryColor)
			case len(got.Nav) != 2 || got.Nav[0].Label != "About" || got.Nav[1].Path != "/blog":
				t.Errorf("the navigation reads back as %+v, and its order is the order it was written in", got.Nav)
			}
		},

		"saving what is already stored says nothing": func(t *testing.T, f Fixture) {
			first := save(t, f, acme())
			var again *contracts.SiteSettings
			f.silent(t, "saving the same settings again", func() { again = save(t, f, acme()) })
			if again.ID != first.ID {
				t.Errorf("the second save made %s where there was %s; a tenant has one site", again.ID, first.ID)
			}
		},

		"changing one thing says so": func(t *testing.T, f Fixture) {
			first := save(t, f, acme())
			changed := acme()
			changed.Nav = append(changed.Nav, contracts.NavItem{Label: "Contact", Path: "/contact"})
			var out *contracts.SiteSettings
			f.one(t, "adding a link", func() { out = save(t, f, changed) })
			if out.ID != first.ID || len(out.Nav) != 3 {
				t.Errorf("the changed settings are %s with %d links", out.ID, len(out.Nav))
			}
		},

		"a colour is #rrggbb and a theme is one of three": func(t *testing.T, f Fixture) {
			// Saved once first, so the refusals below go through the update
			// path as well as the create one: the two used to answer with
			// different errors, which is a 500 where a 422 belongs.
			save(t, f, acme())
			// Every colour here is refused for its *spelling*, which is why none of
			// them is a colour the contrast rule could be accused of refusing: a
			// #rrggbb that reads badly is refused by the case below, by name.
			for _, bad := range []*contracts.SiteSettings{
				{Title: "Acme", PrimaryColor: "red"},
				{Title: "Acme", PrimaryColor: "#fff"},
				{Title: "Acme", PrimaryColor: "rgb(1,2,3)"},
				// Space around nothing is still nothing, and the only thing that makes
				// it a colour is the grammar: the empty colour is a valid stored value,
				// " #" is neither a colour nor an absence of one.
				{Title: "Acme", PrimaryColor: " #"},
				{Title: "Acme", Theme: "neon"},
			} {
				_, err := f.Service.Save(f.Ctx, f.Tx, bad)
				mustBe(t, err, crud.ErrInvalid)
			}
		},

		// The contrast rule, in one case, run by both implementations through the
		// same Save: the fake validates with the entity's own Validate and so does
		// the SQL service, so a rule branched on Theme, or run only on the create
		// path, or run only inside internal/, turns red here rather than passing in
		// one implementation and refusing in the other.
		//
		// Every number beside these colours is the WCAG ratio measured against the
		// kit's two canvases; TestTheAccentRatiosAreTheOnesTheRuleCompared prints
		// them from the same arithmetic the rule used.
		"an accent must read against both canvases": func(t *testing.T, f Fixture) {
			save(t, f, acme())
			for _, colour := range []string{
				"#ff8800", // 2.08 light — the colour this suite used to configure
				"#c0ffee", // 1.03 light — mint on paper
				"#ffffff", // 1.15 light — paper on paper
				"#0e1614", // 1.00 dark — and the dark canvas on itself
				"#1d4ed8", // 2.74 dark — a blue that only works in the light
				"#8b8b8b", // 2.97 light — the grey a hair below the threshold
			} {
				if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: colour}); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("saving %q: error is %v, want crud.ErrInvalid: it reads under %g:1 against one of the two canvases",
						colour, err, contracts.MinAccentRatio)
				}
			}
			for _, colour := range []string{
				"#2563eb", // 4.50 / 3.55 — a blue inside the band, and one a tenant may choose
				"#b45309", // 4.37 / 3.66
				"#0f766e", // 4.76 / 3.36
				"#7c3aed", // 4.96 / 3.22
				"#7f7f7f", // 3.48 / 4.59 — a mid grey clears both by a wider margin than either extreme
				"#8a8a8a", // 3.0044 / 5.32 — the nearest colour above the band's edge
			} {
				if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: colour}); err != nil {
					t.Errorf("saving %q: %v: a colour that reads at least %g:1 against both canvases is accepted", colour, err, contracts.MinAccentRatio)
				}
			}
		},

		// Where the band's edge sits, named rather than rounded away: two greys one
		// step apart, #8a8a8a at 3.0044:1 against the light canvas and #8b8b8b at
		// 2.9651:1. The rule accepts the first and refuses the second, so the edge
		// lies between them. What this case does is pin the band's neighbourhood — no
		// #rrggbb colour reads exactly 3.0 against these two canvases, so it cannot
		// tell `>=` from `>`, and it does not claim to. A comparison made on rounded
		// ratios, though, refuses #8a8a8a, and that is a mutation this order kills.
		"the band's edge sits between #8b8b8b and #8a8a8a": func(t *testing.T, f Fixture) {
			save(t, f, acme())
			if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#8a8a8a"}); err != nil {
				t.Errorf("#8a8a8a reads 3.0044:1 against the light canvas and is refused: %v", err)
			}
			_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#8b8b8b"})
			mustBe(t, err, crud.ErrInvalid)
		},

		// A colour is compared, not guessed: the trim and the lower-case happen
		// first, so the same colour typed two ways is stored one way.
		"a colour typed with room and case is the colour it means": func(t *testing.T, f Fixture) {
			out := save(t, f, &contracts.SiteSettings{Title: "Acme", PrimaryColor: " #2563EB "})
			if out.PrimaryColor != "#2563eb" {
				t.Errorf("the stored colour is %q, want %q", out.PrimaryColor, "#2563eb")
			}
			_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#2563E b"})
			mustBe(t, err, crud.ErrInvalid)
		},

		// The read's answer is a storable document, and saving it back is not an
		// edit: a settings form submits what the read gave it, so a save that stored
		// the installation's accent as this tenant's own would freeze them at the
		// palette of the day, bump the revision, invalidate a cache and put a line in
		// the trail that nobody typed. The colour that goes into the row is the empty
		// one, which is why the second read answers the palette again rather than a
		// value of its own.
		"the read's answer, saved back, is not a change": func(t *testing.T, f Fixture) {
			shown, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if shown.PrimaryColor == contracts.DefaultPrimaryColor {
				t.Fatalf("the read answered the empty colour; every door that shows these settings needs a colour")
			}
			first := save(t, f, shown)
			if first.Revision != 1 {
				t.Errorf("the first save left the row at revision %d", first.Revision)
			}
			again, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			var out *contracts.SiteSettings
			f.silent(t, "saving the read's answer back", func() { out = save(t, f, again) })
			if out.Revision != 1 {
				t.Errorf("echoing the read's answer moved the revision to %d; nothing was chosen, so nothing was written", out.Revision)
			}
			if out.PrimaryColor != InstallationPalette.Accent(contracts.ThemeLight) {
				t.Errorf("the save answered %q, want the installation's accent it was given", out.PrimaryColor)
			}
		},

		// The echo is compared against both of the installation's accents and not
		// against the row's own mode, and this is the case that would catch it being
		// narrowed: a form that changes light to dark sends the accent the read gave
		// for light. Stored as itself that colour meets the legibility rule and is
		// refused — a form that cannot change its theme because it never chose a
		// colour. Clearing it is what lets the theme move and the next read answer the
		// dark accent.
		"the theme can change while the accent stays unchosen": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", Theme: contracts.ThemeLight}); err != nil {
				t.Fatalf("save a light site: %v", err)
			}
			shown, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if shown.PrimaryColor != InstallationPalette.Accent(contracts.ThemeLight) {
				t.Fatalf("a light site with no accent reads %q", shown.PrimaryColor)
			}
			shown.Theme = contracts.ThemeDark
			save(t, f, shown)
			got, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if got.PrimaryColor != InstallationPalette.Accent(contracts.ThemeDark) {
				t.Errorf("the dark site reads %q, want %q: the theme moved and the accent is still nobody's",
					got.PrimaryColor, InstallationPalette.Accent(contracts.ThemeDark))
			}
		},

		// A colour somebody did choose is not the installation's, whichever door it
		// is read from and whatever the installation is drawn with.
		"an accent the tenant chose survives every door": func(t *testing.T, f Fixture) {
			save(t, f, acme())
			got, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if got.PrimaryColor != "#b45309" {
				t.Errorf("Settings answers %q, want the #b45309 this tenant wrote", got.PrimaryColor)
			}
			locked, err := f.Locked.SettingsForUpdate(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("SettingsForUpdate: %v", err)
			}
			if locked.PrimaryColor != got.PrimaryColor || locked.Revision != got.Revision {
				t.Errorf("the locked read answers %q at revision %d and the unlocked read %q at %d",
					locked.PrimaryColor, locked.Revision, got.PrimaryColor, got.Revision)
			}
		},

		// Both reads are the same answer, which is what lets a change proposal be
		// diffed against the document the person was shown: modules/change marshals
		// what SettingsForUpdate returns, and a proposal merged from the API answer
		// therefore round-trips — the merged accent is the one the read answered, and
		// applying it stores the empty colour and moves no revision.
		"the locked read says what the unlocked read says": func(t *testing.T, f Fixture) {
			unlocked, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			locked, err := f.Locked.SettingsForUpdate(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("SettingsForUpdate: %v", err)
			}
			if locked.PrimaryColor != unlocked.PrimaryColor {
				t.Errorf("the locked read answers %q and the plain read %q", locked.PrimaryColor, unlocked.PrimaryColor)
			}
			if locked.Revision != unlocked.Revision {
				t.Errorf("the locked read is at revision %d and the plain read at %d", locked.Revision, unlocked.Revision)
			}
		},

		// The installation's own accent is not a colour this tenant may hold, and the
		// door that receives it is the one that says so: a form that sends either
		// accent back is answered the installation's own colour for the mode it is on,
		// which — for the accent of the *other* mode — is not the value it sent. Stored
		// as a chosen colour that second value would be answered back forever, and the
		// legibility rule behind Save would have refused it as a hand-typed colour
		// (TestTheAccentRatiosAreTheOnesTheRuleCompared measures both accents under
		// the threshold on one canvas each).
		"the installation's own accent is not a tenant's colour": func(t *testing.T, f Fixture) {
			save(t, f, &contracts.SiteSettings{
				Title: "Acme", Theme: contracts.ThemeLight,
				PrimaryColor: InstallationPalette.Accent(contracts.ThemeDark),
			})
			got, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if got.PrimaryColor != InstallationPalette.Accent(contracts.ThemeLight) {
				t.Errorf("a light site sent the installation's dark accent and reads %q, want %q: the value was nobody's choice",
					got.PrimaryColor, InstallationPalette.Accent(contracts.ThemeLight))
			}
		},

		// The two numbers a shell is told are the two the rule compared — same
		// arithmetic, same rounding, one implementation.
		"the ratio the document quotes is the ratio the rule used": func(t *testing.T, f Fixture) {
			light, dark, ok := contracts.AccentRatios("#2563eb")
			if !ok {
				t.Fatal("AccentRatios cannot read a colour this rule has measured for years")
			}
			if light != 4.50 || dark != 3.55 {
				t.Errorf("#2563eb reads %.2f / %.2f, want 4.50 / 3.55", light, dark)
			}
			if _, err := contracts.ContrastRatio("mint", contracts.CanvasLight); err == nil || !strings.Contains(err.Error(), "mint") {
				t.Errorf("ContrastRatio(%q, …) = %v, want an error naming the value", "mint", err)
			}
		},

		"a link points inside this site": func(t *testing.T, f Fixture) {
			for _, path := range []string{"https://evil.example.com", "about-us", ""} {
				_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{
					Title: "Acme", Nav: contracts.Nav{{Label: "About", Path: path}},
				})
				mustBe(t, err, crud.ErrInvalid)
			}
			// And a link needs something to say.
			_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{
				Title: "Acme", Nav: contracts.Nav{{Label: "  ", Path: "/about-us"}},
			})
			mustBe(t, err, crud.ErrInvalid)
		},

		"a navigation is bounded and so is a title": func(t *testing.T, f Fixture) {
			long := contracts.Nav{}
			for range contracts.MaxNav + 1 {
				long = append(long, contracts.NavItem{Label: "Link", Path: "/link"})
			}
			_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", Nav: long})
			mustBe(t, err, crud.ErrInvalid)

			_, err = f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: strings.Repeat("a", contracts.MaxTitle+1)})
			mustBe(t, err, crud.ErrInvalid)
		},

		"a home page is named by a slug": func(t *testing.T, f Fixture) {
			_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", HomeSlug: "Welcome Home"})
			mustBe(t, err, crud.ErrInvalid)
			// Empty is not a refusal: a site is configurable before anything is
			// published at any name.
			if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
				t.Errorf("a site with no home page: %v", err)
			}
		},
	}
}

func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
