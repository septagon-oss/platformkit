package ui_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestExportMeasurementsUseSourceValuesAndRequiredFeature(t *testing.T) {
	child := layoutExample("child", c.FlexProps{Gap: "2"})
	doc := layoutExport(t, []c.Example{layoutExample("root", c.FlexProps{}, child.Node)})
	want, err := style.Measurements()
	if err != nil || !reflect.DeepEqual(doc.Measurements, want) {
		t.Fatalf("export did not project its owning scales: %v", err)
	}
	features := []string{"source-flex-declarations.v1", "source-measurements.v1"}
	if !slices.Equal(doc.RequiredFeatures, features) || doc.CheckLayoutContract(features...) != nil {
		t.Fatal("fresh source measurements have no understood feature contract")
	}
	if !errors.Is(doc.CheckLayoutContract(features[0]), ui.ErrLayoutUnsupported) {
		t.Fatal("old consumer silently ignored the new required meaning")
	}
	// Packaging can retain only the dependency closure. Identity, not array
	// order, resolves both a parent gap and a different nested occurrence's gap.
	doc.Measurements = []style.Measurement{
		{Scale: "spacing", Key: "2", Value: "0.5", Unit: "rem"},
		{Scale: "spacing", Key: "4", Value: "1", Unit: "rem"},
	}
	slices.Reverse(doc.RequiredFeatures)
	if err := doc.CheckLayoutContract(features...); err != nil {
		t.Fatalf("valid dependency-closed subset rejected: %v", err)
	}
	doc.Measurements = doc.Measurements[1:]
	if !errors.Is(doc.CheckLayoutContract(features...), ui.ErrLayoutUnknown) {
		t.Fatal("missing nested gap resolved to an invented default")
	}
}

func TestExportMeasurementsRefuseAmbiguityAndRetainDeclarationOnlyMigration(t *testing.T) {
	base := layoutExport(t, []c.Example{layoutExample("root", c.FlexProps{})})
	features := []string{"source-flex-declarations.v1", "source-measurements.v1"}
	for name, change := range map[string]func(*ui.DesignExport){
		"unrequired data": func(d *ui.DesignExport) { d.RequiredFeatures = d.RequiredFeatures[:1] },
		"duplicate":       func(d *ui.DesignExport) { d.Measurements = append(d.Measurements, d.Measurements[0]) },
		"invalid":         func(d *ui.DesignExport) { d.Measurements[0].Unit = "future" },
		"unknown scale":   func(d *ui.DesignExport) { d.Measurements[0].Scale = "future" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := base
			doc.Measurements = slices.Clone(base.Measurements)
			doc.RequiredFeatures = slices.Clone(base.RequiredFeatures)
			change(&doc)
			if !errors.Is(doc.CheckLayoutContract(features...), ui.ErrLayoutUnsupported) {
				t.Fatal("invalid measurement contract accepted")
			}
		})
	}
	base.RequiredFeatures = base.RequiredFeatures[:1]
	base.Measurements = nil
	if err := base.CheckLayoutContract("source-flex-declarations.v1"); err != nil {
		t.Fatalf("earlier declaration-only v2 was silently redefined: %v", err)
	}
}
