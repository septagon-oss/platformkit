package sitetest

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

func readDefaults(t *testing.T, f Fixture) {
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
	case got.Revision != 0:
		t.Errorf("an unconfigured site is at revision %d; the number counts writes", got.Revision)
	}
}

func saveSettings(t *testing.T, f Fixture) {
	if out := save(t, f, acme()); out.ID == uuid.Nil {
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
}

func changeOneSetting(t *testing.T, f Fixture) {
	first := save(t, f, acme())
	changed := acme()
	changed.Nav = append(changed.Nav, contracts.NavItem{Label: "Contact", Path: "/contact"})
	var out *contracts.SiteSettings
	f.one(t, "adding a link", func() { out = save(t, f, changed) })
	if out.ID != first.ID || len(out.Nav) != 3 {
		t.Errorf("the changed settings are %s with %d links", out.ID, len(out.Nav))
	}
}

func saveWithoutHome(t *testing.T, f Fixture) {
	if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
		t.Errorf("a site with no home page: %v", err)
	}
}

func readLockedSettings(t *testing.T, f Fixture) {
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
}
