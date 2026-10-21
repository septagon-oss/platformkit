package export_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/export"
)

// A consumer addition withdraws a generated identity's layout claim only when it
// paints something: an empty sheet, or class lists alone, declare nothing a flex
// container could lose, so the claim stays admitted and the export is unchanged.
func TestGeneratedIdentityKeepsItsLayoutClaimWhenConsumerExtraPaintsNothing(t *testing.T) {
	t.Parallel()
	pair, err := design.FromSeed(design.Seed{Sector: "publishing", Name: "Common Ground"})
	if err != nil {
		t.Fatal(err)
	}
	child := layoutExample("child", c.FlexProps{})
	captures := []examples.Example{layoutExample("root", c.FlexProps{}, child.Node)}
	plain, err := export.ExportWithLayout(pair, captures)
	if err != nil {
		t.Fatal(err)
	}
	features := []string{"source-flex-declarations.v1", "source-measurements.v1"}
	for name, extra := range map[string]ui.Extra{
		"empty sheet": {Sheets: []*css.Sheet{css.NewSheet()}},
		"lists only":  {Lists: c.ClassLists()},
	} {
		got, err := export.ExportWithLayout(pair, captures, extra)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := got.CheckLayoutContract(features...); err != nil {
			t.Fatalf("%s: a consumer that paints nothing withdrew the layout claim: %v", name, err)
		}
		if !reflect.DeepEqual(plain.Themes, got.Themes) {
			t.Fatalf("%s: the generated identity's tokens moved", name)
		}
	}
	empty, err := export.ExportWithLayout(pair, captures, ui.Extra{Sheets: []*css.Sheet{css.NewSheet()}})
	if err != nil || !reflect.DeepEqual(plain, empty) {
		t.Fatalf("an empty consumer sheet changed the export: %v", err)
	}
}
