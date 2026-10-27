package view_test

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnAmountShownInAFieldACellAndAControlIsOneAmount reads one stored amount out of the
// three zones a page shows a value in — a record's section field, a list's table cell and a
// form's control — and asks that all three write one money: the same minor units, the same
// currency and the same scale. A page spells an amount per zone, so a rewrite that keeps the
// digits in one zone and reshapes them in another can leave every zone true to its own
// expectation while the page contradicts itself, and the sum a person reads in a table would
// not be the amount the form sends back. The amounts are whole numbers on both sides of zero
// and at both ends of the range no double holds exactly, the minor units must read back
// through strconv.ParseInt as the amount that went in, and each zone refuses the case when it
// shows no amount at all, so it cannot pass by inspecting nothing.
func TestAnAmountShownInAFieldACellAndAControlIsOneAmount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		amount any
		digits string
	}{
		{"greatest int64", int64(math.MaxInt64), "9223372036854775807"},
		{"least int64", int64(math.MinInt64), "-9223372036854775808"},
		{"decoded negative", json.Number("-400"), "-400"},
		{"whole double", float64(1e18), "1000000000000000000"},
	} {
		in := domainInput(map[string]any{
			"id": "t1", "title": "n", "amount": tc.amount, "currency": "EUR",
		}, time.UTC)
		zones := []struct {
			name   string
			amount view.Money
		}{
			{"record field", amountInRecordField(t, in)},
			{"list cell", amountInListCell(t, in)},
			{"form control", amountInFormControl(t, in)},
		}
		for _, zone := range zones {
			if zone.amount.MinorUnits != tc.digits || zone.amount.Currency != "EUR" || zone.amount.Scale != 2 {
				t.Errorf("the %s page writes the amount %s as %q of %q at scale %d, want %q of %q at scale 2",
					zone.name, tc.name, zone.amount.MinorUnits, zone.amount.Currency, zone.amount.Scale,
					tc.digits, "EUR")
			}
			if _, err := strconv.ParseInt(zone.amount.MinorUnits, 10, 64); err != nil {
				t.Errorf("the %s page writes minor units %q, which no shell can read back as an amount: %v",
					zone.name, zone.amount.MinorUnits, err)
			}
		}
		if zones[0].amount != zones[1].amount || zones[0].amount != zones[2].amount {
			t.Errorf("the page shows the amount %s as three spellings: field %s, cell %s, control %s of %q at scales %d/%d/%d",
				tc.name, zones[0].amount.MinorUnits, zones[1].amount.MinorUnits, zones[2].amount.MinorUnits,
				zones[0].amount.Currency, zones[0].amount.Scale, zones[1].amount.Scale, zones[2].amount.Scale)
		}
	}
}

// amountInRecordField is the amount a record page shows in its section field, and the case
// fails when the record shows none, because then this zone would be inspected as nothing.
func amountInRecordField(t *testing.T, in view.Input) view.Money {
	t.Helper()
	for _, section := range build(t, in).Sections {
		for _, f := range section.Fields {
			if f.Name == "amount" && f.ValueKind == view.ValueMoney {
				return moneyAmount(t, "the record field", f.Value)
			}
		}
	}
	t.Fatal("the record page shows no money field named amount, so this case would inspect nothing")
	return view.Money{}
}

// amountInListCell is the amount a list page shows in one of a row's cells.
func amountInListCell(t *testing.T, in view.Input) view.Money {
	t.Helper()
	list := in
	list.State.Kind = view.KindList
	for _, row := range build(t, list).Table.Rows {
		for _, cell := range row.Cells {
			if cell.Field == "amount" && cell.ValueKind == view.ValueMoney {
				return moneyAmount(t, "the list cell", cell.Value)
			}
		}
	}
	t.Fatal("the list page shows no money cell named amount, so this case would inspect nothing")
	return view.Money{}
}

// amountInFormControl is the amount a form page starts a control at.
func amountInFormControl(t *testing.T, in view.Input) view.Money {
	t.Helper()
	form := in
	form.State.Kind = view.KindForm
	form.State.Form = view.FormState{Create: true, Title: "New task"}
	for _, c := range build(t, form).Form.Controls {
		if c.Name == "amount" && c.ValueKind == view.ValueMoney {
			return moneyAmount(t, "the form control", c.Value)
		}
	}
	t.Fatal("the form page shows no money control named amount, so this case would inspect nothing")
	return view.Money{}
}

// moneyAmount is a shown value read as the money it declares itself to be.
func moneyAmount(t *testing.T, where string, value any) view.Money {
	t.Helper()
	m, ok := value.(view.Money)
	if !ok {
		t.Fatalf("%s carries the amount as %T, want view.Money", where, value)
	}
	return m
}
