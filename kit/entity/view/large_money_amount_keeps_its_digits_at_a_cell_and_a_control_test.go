package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnAmountBeyondTheDoubleRangeKeepsItsDigitsAtACellAndAControl reads a money amount
// past 2^53, negative, off a list page's cell and a form page's control and asks for the
// stored digits, spelled as a JSON string. A client whose only number type is a double
// loses the last digits of such a sum, which is why Money carries them as text, and a
// builder that helps by spelling them as a number loses them the same way. No reviewed
// fixture reads a money amount off a list page, so such a builder leaves the record's own
// reading and every fixture green and ships a cell whose sum sits one unit off the
// record's. Each sub-case fails outright when its kind carries no money at all, so the
// case cannot pass by inspecting nothing.
func TestAnAmountBeyondTheDoubleRangeKeepsItsDigitsAtACellAndAControl(t *testing.T) {
	t.Parallel()
	const digits = "-9007199254740993"
	in := domainInput(map[string]any{
		"id": "t1", "title": "n", "amount": json.Number(digits), "currency": "EUR",
	}, time.UTC)
	for _, kind := range []view.Kind{view.KindList, view.KindForm} {
		t.Run(string(kind), func(t *testing.T) {
			shown := in
			shown.State.Kind = kind
			if kind == view.KindForm {
				shown.State.Form = view.FormState{Create: true, Title: "New task"}
			}
			page := build(t, shown)
			var value any
			switch kind {
			case view.KindList:
				for _, row := range page.Table.Rows {
					for _, cell := range row.Cells {
						if cell.Field == "amount" && cell.ValueKind == view.ValueMoney {
							value = cell.Value
						}
					}
				}
			case view.KindForm:
				for _, control := range page.Form.Controls {
					if control.Name == "amount" && control.ValueKind == view.ValueMoney {
						value = control.Value
					}
				}
			}
			if value == nil {
				t.Fatalf("the %s page shows no money amount, so this case would inspect nothing", kind)
			}
			var wire struct {
				MinorUnits json.RawMessage `json:"minorUnits"`
			}
			if err := json.Unmarshal(bytes(t, value), &wire); err != nil {
				t.Fatalf("the %s page's amount does not spell money: %v", kind, err)
			}
			if string(wire.MinorUnits) != `"`+digits+`"` {
				t.Errorf("the %s page writes the amount's minor units as %s, want the digits as a JSON string",
					kind, wire.MinorUnits)
			}
		})
	}
}
