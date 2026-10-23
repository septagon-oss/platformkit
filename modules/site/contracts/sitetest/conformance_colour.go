package sitetest

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

func acceptAccentBand(t *testing.T, f Fixture) {
	save(t, f, acme())
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
}

func normaliseColour(t *testing.T, f Fixture) {
	out := save(t, f, &contracts.SiteSettings{Title: "Acme", PrimaryColor: " #2563EB "})
	if out.PrimaryColor != "#2563eb" {
		t.Errorf("the stored colour is %q, want %q", out.PrimaryColor, "#2563eb")
	}
	_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#2563E b"})
	mustBe(t, err, crud.ErrInvalid)
}

func verifyContrastRatio(t *testing.T, f Fixture) {
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
}
