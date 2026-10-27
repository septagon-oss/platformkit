package view_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnIntAmountAtTheEndOfItsRangeKeepsItsDigitsInACellAndAControl reads the money a
// shell actually prints — a table cell and a form control — for an amount a Go caller
// handed over as an int at the ends of the range no double holds. A page's field, cell and
// control are three zones, and a spelling written per zone could keep the digits in one and
// lose them in another, so both ends are asked of every zone that shows a value, and each
// zone refuses the case when it shows no money at all. The amounts convert at run time, not
// as constants: a constant that large does not fit an int where an int is 32 bits, and there
// no amount could be asked.
func TestAnIntAmountAtTheEndOfItsRangeKeepsItsDigitsInACellAndAControl(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("where an int is 32 bits it carries no amount a double cannot hold")
	}
	for _, tc := range []struct {
		name   string
		amount int64
		digits string
	}{
		{"greatest", math.MaxInt64, "9223372036854775807"},
		{"least", math.MinInt64, "-9223372036854775808"},
	} {
		row := map[string]any{"id": "t1", "title": "n", "amount": int(tc.amount), "currency": "EUR"}

		listIn := domainInput(row, time.UTC)
		listIn.State.Kind = view.KindList
		page := build(t, listIn)
		var cells []view.Money
		for _, r := range page.Table.Rows {
			for _, c := range r.Cells {
				if c.ValueKind == view.ValueMoney {
					cells = append(cells, moneyValue(t, "the list cell "+c.Field, c.Value))
				}
			}
		}
		if len(cells) == 0 {
			t.Fatal("the list page shows no money cell, so this case would inspect nothing")
		}

		formIn := domainInput(row, time.UTC)
		formIn.State.Kind = view.KindForm
		formIn.State.Form = view.FormState{Create: true, Title: "New task"}
		var controls []view.Money
		for _, c := range build(t, formIn).Form.Controls {
			if c.ValueKind == view.ValueMoney {
				controls = append(controls, moneyValue(t, "the form control "+c.Name, c.Value))
			}
		}
		if len(controls) == 0 {
			t.Fatal("the form page shows no money control, so this case would inspect nothing")
		}

		for _, m := range append(cells, controls...) {
			if m.MinorUnits != tc.digits || m.Currency != "EUR" || m.Scale != 2 {
				t.Errorf("the page writes the amount int/%s as minor units %q of %q at scale %d, want %q of %q at scale 2",
					tc.name, m.MinorUnits, m.Currency, m.Scale, tc.digits, "EUR")
			}
		}
	}
}

// moneyValue is a page's shown value read as the money it declares itself to be.
func moneyValue(t *testing.T, where string, value any) view.Money {
	t.Helper()
	m, ok := value.(view.Money)
	if !ok {
		t.Fatalf("%s carries money as %T, want view.Money", where, value)
	}
	return m
}
