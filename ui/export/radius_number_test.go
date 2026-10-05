package export_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
)

func TestEveryAcceptedRadiusHasADTCGNumber(t *testing.T) {
	for _, radius := range []string{"1px", "0.5rem", "01px", "00.5rem"} {
		t.Run(radius, func(t *testing.T) {
			shape := design.Shape{CardRadius: radius}
			if err := shape.Validate(); err != nil {
				if radius == "1px" || radius == "0.5rem" {
					t.Fatal(err)
				}
				t.Logf("noncanonical radius refused before export: %v", err)
				return
			}
			pair, err := (design.Client{
				Slug: "lantern", Seed: design.Seed{Sector: "publishing", Name: "Lantern"}, Shape: shape,
			}).Resolve()
			if err != nil {
				t.Fatal(err)
			}
			tokens, err := export.ExportTokens(pair, "light", "dark")
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"light", "dark"} {
				data, _, err := (export.TokenExport{Modes: tokens.Modes, Colors: tokens.Colors}).DTCG(mode)
				if err != nil {
					t.Errorf("accepted radius %q cannot export in %s: %v", radius, mode, err)
				} else if !json.Valid(data) {
					t.Errorf("accepted radius %q exported invalid JSON", radius)
				}
			}
		})
	}
}
