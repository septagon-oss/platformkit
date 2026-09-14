package ui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestTokenSnapshotComposesWithoutChangingLegacyOrLayoutAdmission(t *testing.T) {
	tokens, err := ui.ExportTokens(design.Default(), "light")
	if err != nil {
		t.Fatal(err)
	}
	base, err := ui.Export(design.Default(), examples.Gallery())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(base)
	doc, err := base.WithTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "platformkit.design-export.v2" || !slices.Equal(doc.RequiredFeatures, []string{"source-tokens.v1"}) ||
		doc.SourceTokens == nil || !reflect.DeepEqual(*doc.SourceTokens, tokens) || doc.SHA256 == base.SHA256 {
		t.Fatal("source token opt-in is missing, nondeterministic or not content-addressed")
	}
	if err := doc.CheckSourceContract("source-tokens.v1"); err != nil {
		t.Fatalf("token-only contract incorrectly requires component layout: %v", err)
	}
	if !errors.Is(doc.CheckSourceContract(), ui.ErrSourceUnsupported) {
		t.Fatal("consumer without token support admitted required token contract")
	}
	claimed := doc.SHA256
	doc.SHA256 = ""
	encoded, _ := json.Marshal(doc)
	hash := sha256.Sum256(encoded)
	if claimed != hex.EncodeToString(hash[:]) {
		t.Fatal("digest omitted the source token block or required feature")
	}
	doc.SourceTokens.Modes[0].Fonts[0].Families[0].Name = "changed"
	doc.SourceTokens.Scales[0].Number.Value = "999"
	doc.Themes[0].Tokens[0].Value = "changed"
	doc.Examples[0].Props[0] = ' '
	after, _ := json.Marshal(base)
	fresh, _ := ui.ExportTokens(design.Default(), "light")
	if string(after) != string(before) || !reflect.DeepEqual(tokens, fresh) {
		t.Fatal("composed snapshot aliases caller inputs")
	}
	again, err := base.WithTokens(tokens)
	if err != nil || again.SHA256 != claimed {
		t.Fatalf("same source did not reproduce snapshot: %v", err)
	}
	if _, err := again.WithTokens(tokens); !errors.Is(err, ui.ErrSourceUnsupported) {
		t.Fatalf("repeated attachment was not refused: %v", err)
	}
	layout := layoutExport(t, []examples.Example{layoutExample("root", c.FlexProps{})})
	features := []string{"source-flex-declarations.v1", "source-measurements.v1", "source-tokens.v1"}
	combined, err := layout.WithTokens(tokens)
	if err != nil || combined.CheckSourceContract(features...) != nil {
		t.Fatalf("valid token and layout composition was refused: %v", err)
	}
	if !errors.Is(combined.CheckLayoutContract(features...), ui.ErrLayoutUnsupported) {
		t.Fatal("legacy layout-only gate silently widened to understand tokens")
	}
	for _, scale := range combined.SourceTokens.Scales {
		if scale.Scale == "spacing" && scale.Key == "4" {
			scale.Number.Value = "999"
		}
	}
	if !errors.Is(combined.CheckSourceContract(features...), ui.ErrSourceUnsupported) {
		t.Fatal("token selection contradicted the same identity in source measurements")
	}
	if err := layout.CheckSourceContract(features...); err != nil {
		t.Fatalf("existing layout/measurement contract narrowed: %v", err)
	}
	layout.RequiredFeatures = []string{"source-flex-declarations.v1"}
	layout.Measurements = nil
	if err := layout.CheckSourceContract("source-flex-declarations.v1"); err != nil {
		t.Fatalf("early layout-only v2 was refused: %v", err)
	}
	unknown := layoutExport(t, examples.Gallery())
	combined, err = unknown.WithTokens(tokens)
	if err != nil || !errors.Is(combined.CheckSourceContract(features...), ui.ErrLayoutUnknown) {
		t.Fatalf("unknown layout was concealed or prevented token capture: %v", err)
	}
}

func TestTokenSnapshotRefusesContradictionsAndUnknownFeatureEnvelopes(t *testing.T) {
	t.Parallel()
	tokens, err := ui.ExportTokens(design.Default(), "light")
	if err != nil {
		t.Fatal(err)
	}
	base, err := ui.Export(design.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ui.DesignExport){
		"unknown version":       func(d *ui.DesignExport) { d.Schema = "platformkit.design-export.v9" },
		"unknown feature":       func(d *ui.DesignExport) { d.RequiredFeatures = append(d.RequiredFeatures, "future.v1") },
		"duplicate feature":     func(d *ui.DesignExport) { d.RequiredFeatures = append(d.RequiredFeatures, "source-tokens.v1") },
		"missing feature":       func(d *ui.DesignExport) { d.RequiredFeatures = nil },
		"missing block":         func(d *ui.DesignExport) { d.SourceTokens = nil },
		"mismatched colour":     func(d *ui.DesignExport) { d.SourceTokens.Modes[0].Colors[0].Value = "#abcdef" },
		"missing source colour": func(d *ui.DesignExport) { d.Themes[0].Tokens = d.Themes[0].Tokens[1:] },
		"mismatched family":     func(d *ui.DesignExport) { d.SourceTokens.Modes[0].Fonts[0].Families[0].Name = "Another" },
		"mismatched literal generic": func(d *ui.DesignExport) {
			families := d.SourceTokens.Modes[0].Fonts[0].Families
			families[len(families)-1].Generic = false
		},
		"duplicate source identity":        func(d *ui.DesignExport) { d.Themes[0].Tokens = append(d.Themes[0].Tokens, d.Themes[0].Tokens[0]) },
		"missing source mode":              func(d *ui.DesignExport) { d.Themes = d.Themes[1:] },
		"duplicate source mode":            func(d *ui.DesignExport) { d.Themes = append(d.Themes, d.Themes[0]) },
		"unselected transition dependency": func(d *ui.DesignExport) { d.SourceTokens.Easings = nil },
		"unadvertised measurement":         func(d *ui.DesignExport) { d.Measurements, _ = style.Measurements() },
		"unadvertised layout": func(d *ui.DesignExport) {
			d.Examples = []examples.ExampleDescription{{Layout: &c.LayoutDescription{Kind: "unknown", Reason: "not captured"}}}
		},
		"measurement without layout": func(d *ui.DesignExport) { d.RequiredFeatures = append(d.RequiredFeatures, "source-measurements.v1") },
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := base.WithTokens(tokens)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&doc)
			if err := doc.CheckSourceContract("source-tokens.v1", "source-flex-declarations.v1", "source-measurements.v1", "future.v1"); !errors.Is(err, ui.ErrSourceUnsupported) {
				t.Fatalf("invalid source snapshot admitted or misclassified: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*ui.DesignExport, *ui.TokenExport){
		"invalid selection":  func(_ *ui.DesignExport, s *ui.TokenExport) { s.Modes = nil },
		"conflicting source": func(_ *ui.DesignExport, s *ui.TokenExport) { s.Modes[0].Colors[0].Value = "#abcdef" },
		"unknown schema":     func(d *ui.DesignExport, _ *ui.TokenExport) { d.Schema = "future" },
		"v1 advertises features": func(d *ui.DesignExport, _ *ui.TokenExport) {
			d.RequiredFeatures = []string{"source-flex-declarations.v1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc, _ := ui.Export(design.Default(), nil)
			s, _ := ui.ExportTokens(design.Default(), "light")
			mutate(&doc, &s)
			out, err := doc.WithTokens(s)
			if !errors.Is(err, ui.ErrSourceUnsupported) || !reflect.DeepEqual(out, ui.DesignExport{}) {
				t.Fatalf("invalid attachment returned partial output: %v", err)
			}
		})
	}
}
