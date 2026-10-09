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
// and what has been published so far.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
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
				case got.PrimaryColor != contracts.DefaultPrimaryColor:
					t.Errorf("the colour is %q, want %q", got.PrimaryColor, contracts.DefaultPrimaryColor)
				case got.Title != "" || len(got.Nav) != 0:
					t.Errorf("an unconfigured site says %+v", got)
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
				"#2563eb", // 4.50 / 3.55 — the kit's own default, so the band holds it
				"#b45309", // 4.37 / 3.66
				"#0f766e", // 4.76 / 3.36
				"#7c3aed", // 4.96 / 3.22
				"#7f7f7f", // 3.48 / 4.59 — a mid grey clears both by a wider margin than either extreme
				"#8a8a8a", // 3.00 / 5.32 — and exactly at the threshold is accepted, not refused
			} {
				if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: colour}); err != nil {
					t.Errorf("saving %q: %v: a colour that reads at least %g:1 against both canvases is accepted", colour, err, contracts.MinAccentRatio)
				}
			}
		},

		// The rule's own boundary, named rather than rounded away: two greys one
		// step apart, one side of 3:1 and one side of it. #8a8a8a reads 3.0044:1
		// against the light canvas and is accepted; #8b8b8b reads 2.9651:1 and is
		// refused. A `>` where the rule says `>=`, or a comparison made on rounded
		// ratios, cannot tell them apart in this order.
		"the threshold is >=, and these two greys straddle it": func(t *testing.T, f Fixture) {
			save(t, f, acme())
			if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#8a8a8a"}); err != nil {
				t.Errorf("#8a8a8a reads 3.00:1 against the light canvas and is refused: %v", err)
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

		// The defaults are not caught by the rule they now have to satisfy: an
		// untouched tenant reads the kit's own accent, and saving that value back
		// is silent, which is what refuses a threshold of 4.5 and a rule that ran
		// before Validate filled the default in.
		"the defaults pass the rule and re-save silently": func(t *testing.T, f Fixture) {
			got, err := f.Service.Settings(f.Ctx, f.Tx)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if got.PrimaryColor != contracts.DefaultPrimaryColor {
				t.Fatalf("the default colour is %q, want %q", got.PrimaryColor, contracts.DefaultPrimaryColor)
			}
			if light, dark, ok := contracts.AccentRatios(got.PrimaryColor); !ok || light < contracts.MinAccentRatio || dark < contracts.MinAccentRatio {
				t.Errorf("the kit's own default reads %v:1 / %v:1 and the rule would refuse it", light, dark)
			}
			f.one(t, "the first save of the defaults", func() { save(t, f, got) })
			f.silent(t, "saving the defaults back", func() { save(t, f, got) })
		},

		// The two numbers a shell is told are the two the rule compared — same
		// arithmetic, same rounding, one implementation.
		"the ratio the document quotes is the ratio the rule used": func(t *testing.T, f Fixture) {
			light, dark, ok := contracts.AccentRatios("#2563eb")
			if !ok {
				t.Fatal("AccentRatios cannot read the kit's own default colour")
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
