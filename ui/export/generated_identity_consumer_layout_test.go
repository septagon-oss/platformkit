package export_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/export"
)

func TestGeneratedIdentityKeepsItsTokensWhenConsumerCSSInvalidatesLayout(t *testing.T) {
	t.Parallel()
	pair, err := design.FromSeed(design.Seed{Sector: "publishing", Name: "Common Ground"})
	if err != nil {
		t.Fatal(err)
	}
	child := layoutExample("child", c.FlexProps{})
	captures := []examples.Example{layoutExample("root", c.FlexProps{}, child.Node)}
	before, err := export.ExportWithLayout(pair, captures)
	if err != nil {
		t.Fatal(err)
	}
	features := []string{"source-flex-declarations.v1", "source-measurements.v1"}
	if err := before.CheckLayoutContract(features...); err != nil {
		t.Fatalf("owned layout must be admitted: %v", err)
	}
	sheet := css.NewSheet()
	sheet.Select(".store-layout", css.Decl("flex-direction", css.Literal("column")))
	after, err := export.ExportWithLayout(pair, captures, ui.Extra{Sheets: []*css.Sheet{sheet}})
	if err != nil {
		t.Fatal(err)
	}
	if err := after.CheckLayoutContract(features...); !errors.Is(err, export.ErrLayoutUnknown) {
		t.Fatalf("consumer CSS must invalidate the layout claim: %v", err)
	}
	if after.Examples[0].Layout.Flex != nil || after.Examples[0].Children[0].Description.Layout.Flex != nil {
		t.Fatal("unknown layout retains a root or child declaration")
	}
	if !reflect.DeepEqual(before.Themes, after.Themes) {
		t.Fatal("invalidating layout changed the generated identity's tokens")
	}
	if after.SHA256 == before.SHA256 || after.CSS == before.CSS {
		t.Fatal("consumer CSS did not reach the exported sheet and its digest")
	}
	again, err := export.ExportWithLayout(pair, captures)
	if err != nil || !reflect.DeepEqual(before, again) {
		t.Fatalf("consumer export mutated the caller's reusable identity or captures: %v", err)
	}
}
