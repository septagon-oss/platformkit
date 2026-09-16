package style_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

// A step read back as a value is the literal its utility rule declares; the
// two come from one table, and this is what keeps an element rule written
// with a step from drifting away from the class that names the same step.
func TestScaleValuesAgreeWithTheUtilityRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ class, property, value string }{
		{"p-8", "padding", style.S8.Value()},
		{"p-0", "padding", style.S0.Value()},
		{"p-px", "padding", style.SPX.Value()},
		{"w-full", "width", style.SFull.Value()},
		{"m-auto", "margin", style.SAuto.Value()},
		{"text-3xl", "font-size", style.Text3XL.Value()},
		{"text-xl", "font-size", style.TextXL.Value()},
		{"rounded-lg", "border-radius", style.RadiusLG.Value()},
		{"rounded", "border-radius", style.RadiusBase.Value()},
		{"border", "border-width", style.Border1.Value()},
		{"border-4", "border-width", style.Border4.Value()},
	} {
		sheet, err := style.Rules(tc.class)
		if err != nil {
			t.Fatalf("%s: %v", tc.class, err)
		}
		if tc.value == "" || !strings.Contains(sheet.CSS(), tc.property+": "+tc.value+";") {
			t.Errorf("%s declares %q, but the step reads back as %q:\n%s", tc.class, tc.property, tc.value, sheet.CSS())
		}
	}
	if style.S8.Value() != "2rem" || style.Text2XL.Value() != "1.5rem" || style.RadiusLG.Value() != "0.5rem" || style.Border4.Value() != "4px" {
		t.Error("the documented values changed; update the callers that chose these steps for their lengths")
	}
}
