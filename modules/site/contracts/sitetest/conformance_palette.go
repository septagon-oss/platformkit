package sitetest

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

func echoSettings(t *testing.T, f Fixture) {
	shown, err := f.Service.Settings(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if shown.PrimaryColor == contracts.DefaultPrimaryColor {
		t.Fatalf("the read answered the empty colour; every door that shows these settings needs a colour")
	}
	if first := save(t, f, shown); first.Revision != 1 {
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
	if out.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
		t.Errorf("the save answered %q, want the installation's accent it was given", out.PrimaryColor)
	}
}

func changeUnchosenTheme(t *testing.T, f Fixture) {
	if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", Theme: contracts.ThemeLight}); err != nil {
		t.Fatalf("save a light site: %v", err)
	}
	shown, err := f.Service.Settings(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if shown.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
		t.Fatalf("a light site with no accent reads %q", shown.PrimaryColor)
	}
	shown.Theme = contracts.ThemeDark
	save(t, f, shown)
	got, err := f.Service.Settings(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if got.PrimaryColor != f.Palette.Accent(contracts.ThemeDark) {
		t.Errorf("the dark site reads %q, want %q: the theme moved and the accent is still nobody's",
			got.PrimaryColor, f.Palette.Accent(contracts.ThemeDark))
	}
}

func preserveChosenAccent(t *testing.T, f Fixture) {
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
}

func distinguishInstallationAccent(t *testing.T, f Fixture) {
	save(t, f, &contracts.SiteSettings{
		Title: "Acme", Theme: contracts.ThemeLight,
		PrimaryColor: f.Palette.Accent(contracts.ThemeDark),
	})
	got, err := f.Service.Settings(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if got.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
		t.Errorf("a light site sent the installation's dark accent and reads %q, want %q: the value was nobody's choice",
			got.PrimaryColor, f.Palette.Accent(contracts.ThemeLight))
	}
}
