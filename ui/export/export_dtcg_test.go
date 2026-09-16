package export_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/style"
)

func interchangeTokens() export.TokenExport {
	return export.TokenExport{
		Modes: []export.TokenMode{{Mode: "light", Colors: []design.Token{{Name: "--brand", Type: "color", Value: "#f008"}},
			Fonts: []design.FontFamilyToken{{Name: "--body", Families: []design.FontFamily{{Name: "Named, Family"}, {Name: "serif"}, {Name: "serif", Generic: true}}}}}},
		Colors: []design.ColorToken{
			{Name: "--alias", Value: design.ColorValue{Reference: "--brand"}},
			{Name: "--mix", Value: design.ColorValue{Mix: &design.ColorMix{First: design.ColorValue{Reference: "--brand"}, FirstPercent: 25, Second: design.ColorValue{Literal: "transparent"}}}},
		},
		Scales: []style.ScaleValue{
			{Scale: "spacing", Key: "0.5", Number: &style.Scalar{Value: "0.125", Unit: "rem"}},
			{Scale: "spacing", Key: "0", Number: &style.Scalar{Value: "0", Unit: "px"}},
			{Scale: "line-height", Key: "base", Number: &style.Scalar{Value: "1.5", Unit: "rem"}},
			{Scale: "leading", Key: "normal", Number: &style.Scalar{Value: "1.5", Unit: ""}},
			{Scale: "font-weight", Key: "normal", Number: &style.Scalar{Value: "650.5", Unit: ""}},
			{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "0.15", Unit: "s"}},
		},
		Easings: []style.EasingValue{{Key: "linear", Keyword: "linear"}, {Key: "in", CubicBezier: &[4]float64{0.4, -2, 1, 3}}},
		Shadows: []style.ShadowValue{{Key: "base", Layers: []style.ShadowLayer{
			{OffsetX: style.Scalar{Value: "-1", Unit: "px"}, OffsetY: style.Scalar{Value: "2", Unit: "rem"}, Color: design.SRGBA{1, 0, 0, 0.3}},
			{OffsetX: style.Scalar{Value: "0", Unit: "px"}, OffsetY: style.Scalar{Value: "1", Unit: "px"}, Blur: style.Scalar{Value: "2", Unit: "px"}, Spread: style.Scalar{Value: "-1", Unit: "px"}, Color: design.SRGBA{0, 0, 1, 0.15}, Inset: true},
		}}},
	}
}

func jsonAt(t *testing.T, document any, path ...string) any {
	t.Helper()
	for _, part := range path {
		object, ok := document.(map[string]any)
		if !ok {
			t.Fatalf("expected object before %q at %q", part, path)
		}
		document, ok = object[part]
		if !ok {
			t.Fatalf("missing %q at %q", part, path)
		}
	}
	return document
}

func TestDTCGPreservesValuesReferencesAndUnambiguousSourcePaths(t *testing.T) {
	t.Parallel()
	source := interchangeTokens()
	before, _ := json.Marshal(source)
	data, diagnostics, err := source.DTCG("light")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path []string
		want any
	}{
		{[]string{"colors", "--alias", "$value"}, "{colors.--brand}"},
		{[]string{"colors", "--mix", "$value", "components"}, []any{float64(1), float64(0), float64(0)}},
		{[]string{"colors", "--mix", "$value", "alpha"}, float64(2) / 15},
		{[]string{"colors", "--brand", "$value", "colorSpace"}, "srgb"},
		{[]string{"fonts", "--body", "$value"}, []any{"Named, Family", "serif", "serif"}},
		{[]string{"scales", "spacing", "0%2E5", "$value"}, map[string]any{"value": 0.125, "unit": "rem"}},
		{[]string{"scales", "spacing", "0%2E5", "$extensions", "dev.septagon.platformkit", "sourcePath"}, []any{"scales", "spacing", "0.5"}},
		{[]string{"scales", "spacing", "0", "$value", "unit"}, "px"},
		{[]string{"scales", "line-height", "base", "$type"}, "dimension"},
		{[]string{"scales", "leading", "normal", "$type"}, "number"},
		{[]string{"scales", "font-weight", "normal", "$type"}, "fontWeight"},
		{[]string{"scales", "font-weight", "normal", "$value"}, 650.5},
		{[]string{"scales", "duration", "150", "$value"}, map[string]any{"value": 0.15, "unit": "s"}},
		{[]string{"easings", "linear", "$value"}, []any{float64(0), float64(0), float64(1), float64(1)}},
		{[]string{"easings", "in", "$value"}, []any{0.4, float64(-2), float64(1), float64(3)}},
		{[]string{"$extensions", "dev.septagon.platformkit", "mode"}, "light"},
	} {
		if got := jsonAt(t, doc, tc.path...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q = %#v, want %#v", tc.path, got, tc.want)
		}
	}
	layers := jsonAt(t, doc, "shadows", "base", "$value").([]any)
	if len(layers) != 2 || jsonAt(t, layers[0], "color", "alpha") != 0.3 || jsonAt(t, layers[1], "inset") != true ||
		jsonAt(t, layers[1], "spread", "value") != float64(-1) || jsonAt(t, layers[0], "blur", "unit") != "px" {
		t.Fatal("shadow order, signed lengths, alpha or explicit default units changed")
	}
	var codes []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Unsupported || len(diagnostic.Path) == 0 || diagnostic.Message == "" {
			t.Fatalf("invalid successful diagnostic: %+v", diagnostic)
		}
		codes = append(codes, diagnostic.Code)
	}
	slices.Sort(codes)
	if !slices.Equal(codes, []string{"font-family-kinds", "linear-keyword", "resolved-color-mix", "shadow-default-length"}) {
		t.Fatalf("declaration loss was not diagnosed: %q", codes)
	}
	ext := jsonAt(t, doc, "colors", "--mix", "$extensions", "dev.septagon.platformkit")
	if jsonAt(t, ext, "source", "value", "mix", "first", "reference") != "--brand" ||
		len(jsonAt(t, doc, "$extensions", "dev.septagon.platformkit", "diagnostics").([]any)) != len(diagnostics) {
		t.Fatal("document dropped source derivation or diagnostics")
	}
	after, _ := json.Marshal(source)
	slices.Reverse(source.Scales)
	slices.Reverse(source.Colors)
	slices.Reverse(source.Easings)
	again, againDiagnostics, err := source.DTCG("light")
	if err != nil || !bytes.Equal(data, again) || !reflect.DeepEqual(diagnostics, againDiagnostics) || !bytes.Equal(before, after) {
		t.Fatalf("interchange is nondeterministic or mutated its source: %v", err)
	}
}

func TestDTCGRefusesAllUnsupportedValuesWithoutPartialDocument(t *testing.T) {
	t.Parallel()
	source := interchangeTokens()
	source.Scales = append(source.Scales,
		style.ScaleValue{Scale: "spacing", Key: "auto", Keyword: "auto"},
		style.ScaleValue{Scale: "spacing", Key: "full", Number: &style.Scalar{Value: "100", Unit: "%"}},
		style.ScaleValue{Scale: "max-width", Key: "prose", Number: &style.Scalar{Value: "65", Unit: "ch"}},
		style.ScaleValue{Scale: "tracking", Key: "tighter", Number: &style.Scalar{Value: "-0.05", Unit: "em"}},
	)
	source.Transitions = []style.TransitionValue{{Key: "colors", Properties: []string{"color"}, Duration: "150", Easing: "in"}, {Key: "none", Properties: []string{"none"}}}
	source.Shadows[0].Layers[0].OffsetX.Unit = "vw"
	source.Modes[0].Fonts[0].Families[0].Name = "{colors.--brand}"
	data, diagnostics, err := source.DTCG("light")
	if !errors.Is(err, export.ErrDTCGUnsupported) || data != nil {
		t.Fatalf("unsupported selection emitted a partial document: %v", err)
	}
	var paths []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Unsupported {
			paths = append(paths, strings.Join(diagnostic.Path, "/"))
		}
	}
	slices.Sort(paths)
	want := []string{"modes/light/fonts/--body", "scales/max-width/prose", "scales/spacing/auto", "scales/spacing/full", "scales/tracking/tighter", "shadows/base", "transitions/colors", "transitions/none"}
	if !slices.Equal(paths, want) {
		t.Fatalf("unsupported selection was not fully diagnosed: %q", paths)
	}
}

func TestDTCGValidatesSourceClosureAndRequiresExplicitMode(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*export.TokenExport){
		"missing colour":               func(s *export.TokenExport) { s.Modes[0].Colors = nil },
		"wrong reference kind":         func(s *export.TokenExport) { s.Colors[0].Value.Reference = "--body" },
		"duplicate identity":           func(s *export.TokenExport) { s.Scales = append(s.Scales, s.Scales[0]) },
		"cycle":                        func(s *export.TokenExport) { s.Colors[0].Value.Reference = "--alias" },
		"infinite number":              func(s *export.TokenExport) { s.Scales[0].Number.Value = "1e999" },
		"weight above exact boundary":  func(s *export.TokenExport) { s.Scales[4].Number.Value = "1000.00000000000000001" },
		"weight below exact boundary":  func(s *export.TokenExport) { s.Scales[4].Number.Value = "0.99999999999999999999" },
		"negative dimension underflow": func(s *export.TokenExport) { s.Scales[0].Number.Value = "-1e-999" },
		"negative duration underflow":  func(s *export.TokenExport) { s.Scales[5].Number.Value = "-1e-999" },
		"negative blur underflow":      func(s *export.TokenExport) { s.Shadows[0].Layers[1].Blur.Value = "-1e-999" },
	} {
		t.Run(name, func(t *testing.T) {
			s := interchangeTokens()
			mutate(&s)
			if data, _, err := s.DTCG("light"); err == nil || data != nil {
				t.Fatalf("invalid source yielded a document: %v", err)
			}
		})
	}
	for _, mode := range []string{"", "both", "dark", "LIGHT"} {
		if data, _, err := interchangeTokens().DTCG(mode); err == nil || data != nil {
			t.Fatalf("unknown/unselected mode %q yielded a document: %v", mode, err)
		}
	}
	source := interchangeTokens()
	source.Modes = append(source.Modes, export.TokenMode{Mode: "dark", Colors: slices.Clone(source.Modes[0].Colors), Fonts: source.Modes[0].Fonts})
	source.Modes[1].Colors[0].Value = "#00f"
	light, _, err := source.DTCG("light")
	if err != nil {
		t.Fatal(err)
	}
	dark, _, err := source.DTCG("dark")
	if err != nil || bytes.Equal(light, dark) {
		t.Fatalf("explicit mode did not affect resolved values: %v", err)
	}
	var doc any
	if err := json.Unmarshal(dark, &doc); err != nil || jsonAt(t, doc, "colors", "--mix", "$value", "alpha") != 0.25 ||
		!reflect.DeepEqual(jsonAt(t, doc, "colors", "--mix", "$value", "components"), []any{float64(0), float64(0), float64(1)}) {
		t.Fatalf("wrong mode resolution: %v", err)
	}
}

func TestDTCGKeepsAssetOnlyEvidenceWithoutInventingFontTokens(t *testing.T) {
	t.Parallel()
	asset := design.Asset{ID: "a", SHA256: strings.Repeat("a", 64), MediaType: "font/woff2", Source: "owner:a",
		License: design.LicenseEvidence{ID: "LicenseRef-Owner", SHA256: strings.Repeat("b", 64), Source: "owner:notice"}}
	second := asset
	second.ID = "b"
	source := export.TokenExport{Assets: []design.Asset{second, asset}, Faces: []design.FontFace{{ID: "a", Asset: "a", Family: "Body", PostScriptName: "Body-Regular", Weight: "650.5", Style: "normal"}}}
	data, diagnostics, err := source.DTCG("")
	if err != nil || len(diagnostics) != 3 {
		t.Fatalf("asset-only package lost metadata diagnostics: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil || len(doc) != 1 {
		t.Fatalf("asset evidence became fabricated DTCG tokens: %v", err)
	}
	assets := jsonAt(t, doc, "$extensions", "dev.septagon.platformkit", "assets").([]any)
	if len(assets) != 2 || jsonAt(t, assets[0], "id") != "a" || jsonAt(t, assets[0], "license", "sha256") != asset.License.SHA256 {
		t.Fatal("asset identity, license evidence or deterministic order changed")
	}
	slices.Reverse(source.Assets)
	again, _, err := source.DTCG("")
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("unordered evidence changed the document: %v", err)
	}
	if data, _, err := source.DTCG("light"); err == nil || data != nil {
		t.Fatal("mode-independent package invented a mode")
	}
}

func TestDTCGKeepsExactDecimalsAndNegativeZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ scale, key, value, unit string }{
		{"font-weight", "normal", "999.99999999999999999", ""},
		{"spacing", "0", "-0E-999", "px"},
		{"tracking", "tighter", "-1e-999", "px"},
	} {
		source := export.TokenExport{Scales: []style.ScaleValue{{Scale: tc.scale, Key: tc.key, Number: &style.Scalar{Value: json.Number(tc.value), Unit: tc.unit}}}}
		data, diagnostics, err := source.DTCG("")
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("decimal %s lost exact representation or valid sign: %v", tc.value, err)
		}
		var doc any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			t.Fatal(err)
		}
		value := jsonAt(t, doc, "scales", tc.scale, tc.key, "$value")
		if tc.unit != "" {
			value = jsonAt(t, value, "value")
		}
		if value != json.Number(tc.value) {
			t.Fatalf("interchange value = %v, want exact %s", value, tc.value)
		}
	}
}

func ExampleTokenExport_DTCG() {
	owned, err := export.ExportTokens(design.Default(), "light")
	if err != nil {
		panic(err)
	}
	// Select a palette/family package explicitly; no implicit filtering of scales.
	selection := export.TokenExport{Modes: owned.Modes, Colors: owned.Colors}
	data, diagnostics, err := selection.DTCG("light")
	fmt.Println(json.Valid(data), len(diagnostics) > 0, err)
	// Output: true true <nil>
}
