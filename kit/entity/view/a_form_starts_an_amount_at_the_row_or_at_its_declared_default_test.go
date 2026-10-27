package view_test

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFormStartsAnAmountAtTheRowOrAtItsDeclaredDefault reads where a money control
// starts: the row's own amount and its currency when the row carries one, and the
// field's declared default when it does not. A default is text on the schema and the
// amount is minor units on the page, so the leg between them is where an amount can
// change shape before anyone typed it, and a module declares a money default today
// (billing's plan price declares "0"). The starting amount must still read back through
// strconv.ParseInt at the declared scale, and a row's amount must win over the default:
// a form that starts a record at the schema's number rather than the record's own
// invites the person to write the default back over the stored amount.
func TestAFormStartsAnAmountAtTheRowOrAtItsDeclaredDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		row      map[string]any
		units    string
		currency string
	}{
		{
			name:     "the row's amount wins over the declared default",
			row:      map[string]any{"id": "t1", "title": "n", "amount": json.Number("9007199254740993"), "currency": "EUR"},
			units:    "9007199254740993",
			currency: "EUR",
		},
		{
			name:     "a row without an amount starts at the default",
			row:      map[string]any{"id": "t1", "title": "n", "currency": "EUR"},
			units:    "2500",
			currency: "",
		},
	} {
		control := amountControl(t, tc.row)
		money, ok := control.Value.(view.Money)
		if !ok {
			t.Fatalf("%s: the amount starts as %T, want view.Money", tc.name, control.Value)
		}
		if money.MinorUnits != tc.units || money.Scale != 2 || money.Currency != tc.currency {
			t.Errorf("%s: the amount starts as %q of %q at scale %d, want %q of %q at scale 2",
				tc.name, money.MinorUnits, money.Currency, money.Scale, tc.units, tc.currency)
		}
		if _, err := strconv.ParseInt(money.MinorUnits, 10, 64); err != nil {
			t.Errorf("%s: the starting amount %q is not minor units any shell can read back: %v",
				tc.name, money.MinorUnits, err)
		}
		if control.Default != "2500" {
			t.Errorf("%s: the control carries default %q, want the field's declared 2500, which is what a shell re-offers when the person clears the field",
				tc.name, control.Default)
		}
	}
}

// amountControl is the page's amount control on a create form over row, with the field
// declaring 2500 as its default; it fails when no amount control exists at all.
func amountControl(t *testing.T, row map[string]any) view.Control {
	t.Helper()
	in := domainInput(row, time.UTC)
	s := in.Resource.Schema
	for i := range s.Fields {
		if s.Fields[i].Name == "amount" {
			s.Fields[i].Default = "2500"
			s.Fields[i].Presentation.Money = &entity.FieldMoney{CurrencyField: "currency", Scale: 2}
		}
	}
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	in.State.Kind = view.KindForm
	in.State.Form = view.FormState{Create: true, Title: "New task"}
	for _, c := range build(t, in).Form.Controls {
		if c.Name == "amount" {
			return c
		}
	}
	t.Fatal("the form shows no amount control, so this case would inspect nothing")
	return view.Control{}
}
