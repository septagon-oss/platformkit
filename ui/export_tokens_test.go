package ui_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/style"
)

func selectedTokens() ui.TokenExport {
	return ui.TokenExport{
		Modes: []ui.TokenMode{{
			Mode:   "light",
			Colors: []design.Token{{Name: "--brand", Type: "color", Value: "#123456"}},
			Fonts:  []design.FontFamilyToken{{Name: "--font", Families: []design.FontFamily{{Name: "serif"}, {Name: "serif", Generic: true}}}},
		}},
		Colors:      []design.ColorToken{{Name: "--action", Value: design.ColorValue{Reference: "--brand"}}},
		Scales:      []style.ScaleValue{{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "150", Unit: "ms"}}},
		Easings:     []style.EasingValue{{Key: "in-out", CubicBezier: &[4]float64{0.4, 0, 0.2, 1}}},
		Transitions: []style.TransitionValue{{Key: "colors", Properties: []string{"color", "background-color"}, Duration: "150", Easing: "in-out"}},
	}
}

func TestTokenSelectionIsDependencyClosedWithoutComponents(t *testing.T) {
	t.Parallel()
	selection := selectedTokens()
	before, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := selection.Validate(); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(selection)
	if string(before) != string(after) {
		t.Fatal("validation mutated the caller's selected values")
	}
	for name, mutate := range map[string]func(*ui.TokenExport){
		"unknown mode":         func(s *ui.TokenExport) { s.Modes[0].Mode = "sepia" },
		"duplicate mode":       func(s *ui.TokenExport) { s.Modes = append(s.Modes, s.Modes[0]) },
		"missing colour":       func(s *ui.TokenExport) { s.Modes[0].Colors = nil },
		"wrong colour kind":    func(s *ui.TokenExport) { s.Modes[0].Colors[0].Type = "dimension" },
		"wrong reference kind": func(s *ui.TokenExport) { s.Colors[0].Value.Reference = "--font" },
		"cycle":                func(s *ui.TokenExport) { s.Colors[0].Value.Reference = "--action" },
		"unsupported literal":  func(s *ui.TokenExport) { s.Modes[0].Colors[0].Value = "currentColor" },
		"duplicate colour":     func(s *ui.TokenExport) { s.Colors = append(s.Colors, s.Colors[0]) },
		"duplicate font":       func(s *ui.TokenExport) { s.Modes[0].Fonts = append(s.Modes[0].Fonts, s.Modes[0].Fonts[0]) },
		"cross-kind collision": func(s *ui.TokenExport) { s.Modes[0].Fonts[0].Name = "--brand" },
		"invalid font":         func(s *ui.TokenExport) { s.Modes[0].Fonts[0].Families[1].Name = "SERIF" },
		"roles without a mode": func(s *ui.TokenExport) { s.Modes = nil },
		"different mode interface": func(s *ui.TokenExport) {
			s.Modes = append(s.Modes, ui.TokenMode{Mode: "dark", Colors: s.Modes[0].Colors})
		},
		"unselected duration":  func(s *ui.TokenExport) { s.Scales = nil },
		"unselected easing":    func(s *ui.TokenExport) { s.Easings = nil },
		"duplicate scale":      func(s *ui.TokenExport) { s.Scales = append(s.Scales, s.Scales[0]) },
		"invalid scale":        func(s *ui.TokenExport) { s.Scales[0].Number.Unit = "px" },
		"duplicate easing":     func(s *ui.TokenExport) { s.Easings = append(s.Easings, s.Easings[0]) },
		"invalid easing":       func(s *ui.TokenExport) { s.Easings[0].CubicBezier[0] = -1 },
		"duplicate transition": func(s *ui.TokenExport) { s.Transitions = append(s.Transitions, s.Transitions[0]) },
		"invalid transition":   func(s *ui.TokenExport) { s.Transitions[0].Properties = nil },
		"invalid shadow":       func(s *ui.TokenExport) { s.Shadows = []style.ShadowValue{{Key: "base"}} },
		"missing face asset": func(s *ui.TokenExport) {
			s.Faces = []design.FontFace{{ID: "body", Asset: "missing", Family: "Body", PostScriptName: "Body-Regular", Weight: "400", Style: "normal"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := selectedTokens()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid selection was admitted")
			}
		})
	}
}

func TestTokenSelectionKeepsIndependentPackagesAndEqualIdentities(t *testing.T) {
	t.Parallel()
	asset := design.Asset{ID: "a", SHA256: strings.Repeat("a", 64), MediaType: "font/woff2", Source: "owner:a",
		License: design.LicenseEvidence{ID: "LicenseRef-Owner", SHA256: strings.Repeat("b", 64), Source: "owner:notice"}}
	for name, selection := range map[string]ui.TokenExport{
		"empty":                                   {},
		"scales without modes":                    {Scales: selectedTokens().Scales},
		"assets without fonts or modes":           {Assets: []design.Asset{asset}},
		"static metadata outside provider subset": {Assets: []design.Asset{asset}, Faces: []design.FontFace{{ID: "a", Asset: "a", Family: "Body", PostScriptName: "Body-Regular", Weight: "650.5", Style: "normal"}}},
		"none needs no timing":                    {Transitions: []style.TransitionValue{{Key: "none", Properties: []string{"none"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := selection.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	s := selectedTokens()
	s.Colors = append(s.Colors, design.ColorToken{Name: "--same-value-distinct-id", Value: s.Colors[0].Value})
	s.Modes = append(s.Modes, ui.TokenMode{Mode: "dark", Colors: slices.Clone(s.Modes[0].Colors), Fonts: s.Modes[0].Fonts})
	s.Modes[1].Colors[0].Value = "#abcdef"
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Colors) != 2 || len(s.Modes[0].Fonts[0].Families) != 2 {
		t.Fatal("equal colours or literal/generic family names were merged")
	}
}

func TestExportTokensSelectsModesAndExistingOwnersDeterministically(t *testing.T) {
	t.Parallel()
	theme := design.Default()
	theme.Light.AccentDefault = "#abcdef"
	theme.Light.Typography.Body = `"Named, Family", "serif", serif`
	first, err := ui.ExportTokens(theme, "dark", "light")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ui.ExportTokens(theme, "light", "dark")
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("mode selection order changed projection: %v", err)
	}
	if first.Modes[0].Mode != "light" || first.Modes[1].Mode != "dark" {
		t.Fatal("source mode order differs from theme selectors")
	}
	for _, mode := range first.Modes {
		if len(mode.Colors) != 22 || len(mode.Fonts) != 3 {
			t.Fatalf("incomplete colour/family projection for %s", mode.Mode)
		}
		for _, color := range mode.Colors {
			if color.Name == "--pk-color-accent-default" && mode.Mode == "light" && color.Value != "#abcdef" {
				t.Fatal("caller palette was not projected")
			}
		}
	}
	wantFamilies := []design.FontFamily{{Name: "Named, Family"}, {Name: "serif"}, {Name: "serif", Generic: true}}
	if !reflect.DeepEqual(first.Modes[0].Fonts[1].Families, wantFamilies) {
		t.Fatalf("fallback order/meaning changed: %+v", first.Modes[0].Fonts)
	}
	scales, _ := style.ScaleValues()
	shadows, _ := style.ShadowValues()
	easings, _ := style.EasingValues()
	transitions, _ := style.TransitionValues()
	if !reflect.DeepEqual(first.Colors, style.RoleColors()) || !reflect.DeepEqual(first.Scales, scales) ||
		!reflect.DeepEqual(first.Shadows, shadows) || !reflect.DeepEqual(first.Easings, easings) || !reflect.DeepEqual(first.Transitions, transitions) {
		t.Fatal("token projection diverged from existing declaration owners")
	}
	if len(first.Assets) != 0 || len(first.Faces) != 0 {
		t.Fatal("projection invented supplied assets or available faces")
	}
	first.Modes[0].Colors[0].Value = "#000"
	first.Modes[0].Fonts[0].Families[0].Name = "Changed"
	first.Scales[0].Number.Value = "999"
	first.Shadows[0].Layers[0].Color[3] = 0.9
	first.Transitions[0].Properties[0] = "--changed"
	for _, easing := range first.Easings {
		if easing.CubicBezier != nil {
			easing.CubicBezier[0] = 0.9
		}
	}
	for _, color := range first.Colors {
		if color.Value.Mix != nil {
			color.Value.Mix.FirstPercent = 12
		}
	}
	again, err := ui.ExportTokens(theme, "light", "dark")
	if err != nil || !reflect.DeepEqual(again, second) {
		t.Fatalf("returned nested values alias source owners: %v", err)
	}
}

func TestExportTokensRefusesWithoutPartialOutputAndIgnoresUnselectedModes(t *testing.T) {
	t.Parallel()
	for _, modes := range [][]string{nil, {""}, {"LIGHT"}, {"light", "light"}, {"light", "sepia"}} {
		out, err := ui.ExportTokens(design.Default(), modes...)
		if err == nil || !reflect.DeepEqual(out, ui.TokenExport{}) {
			t.Fatalf("modes %q yielded partial or successful output: %v", modes, err)
		}
	}
	theme := design.Default()
	theme.Dark.Typography.Body = `var(--font)`
	if out, err := ui.ExportTokens(theme, "light"); err != nil || len(out.Modes) != 1 || out.Modes[0].Mode != "light" {
		t.Fatalf("unselected mode prevented standalone light package: %v", err)
	}
	if out, err := ui.ExportTokens(theme, "light", "dark"); err == nil || !reflect.DeepEqual(out, ui.TokenExport{}) {
		t.Fatalf("bad selected typography yielded partial or successful output: %v", err)
	}
	theme = design.Default()
	theme.Dark.AccentDefault = "currentColor"
	if out, err := ui.ExportTokens(theme, "light", "dark"); err == nil || !reflect.DeepEqual(out, ui.TokenExport{}) {
		t.Fatalf("bad selected colour yielded partial or successful output: %v", err)
	}
}
