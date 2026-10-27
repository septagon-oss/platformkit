package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestTheDeclaredMoneyScaleReachesACellAndAControl reads the schema's two-scale amount
// off a list page's cell and a form page's control and asks for the scale the field
// declares. A shell paints an amount by dividing its minor units by 10^scale, so a
// dropped scale is not a missing detail but an amount a hundred times too large, and
// it is the one part of the money reading a shell cannot recover from the text. The
// record page's own field is where the scale is already checked; this is the same fact
// at the two kinds a phone and a terminal paint from.
func TestTheDeclaredMoneyScaleReachesACellAndAControl(t *testing.T) {
	t.Parallel()
	in := domainInput(map[string]any{
		"id": "t1", "title": "n", "amount": json.Number("400"), "currency": "EUR",
	}, time.UTC)
	for _, kind := range []view.Kind{view.KindList, view.KindForm} {
		t.Run(string(kind), func(t *testing.T) {
			shown := in
			shown.State.Kind = kind
			if kind == view.KindForm {
				shown.State.Form = view.FormState{Create: true, Title: "New task"}
			}
			page := build(t, shown)
			var money view.Money
			switch kind {
			case view.KindList:
				found := false
				for _, row := range page.Table.Rows {
					for _, cell := range row.Cells {
						if cell.Field == "amount" && cell.ValueKind == view.ValueMoney {
							money, found = cell.Value.(view.Money), true
						}
					}
				}
				if !found {
					t.Fatal("the list page shows no money amount, so this case would inspect nothing")
				}
			case view.KindForm:
				found := false
				for _, control := range page.Form.Controls {
					if control.Name == "amount" && control.ValueKind == view.ValueMoney {
						money, found = control.Value.(view.Money), true
					}
				}
				if !found {
					t.Fatal("the form page shows no money amount, so this case would inspect nothing")
				}
			}
			if money.MinorUnits != "400" || money.Currency != "EUR" || money.Scale != 2 {
				t.Errorf("the %s page's amount is %s minor units of %q at scale %d, want 400 of %q at the declared scale 2",
					kind, money.MinorUnits, money.Currency, money.Scale, "EUR")
			}
		})
	}
}
