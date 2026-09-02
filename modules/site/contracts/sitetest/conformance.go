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

// acme is a site somebody has configured.
func acme() *contracts.SiteSettings {
	return &contracts.SiteSettings{
		Title: "Acme", Tagline: "We make things", HomeSlug: "welcome",
		Theme: contracts.ThemeDark, PrimaryColor: "#ff8800",
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
			case got.Theme != contracts.ThemeDark || got.PrimaryColor != "#ff8800":
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
