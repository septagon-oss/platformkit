package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestTheValueDomainReadsTheSameAtEveryPageKind holds what Cell's and Control's own
// comments promise: a Cell and a Control carry the same readings a record's Field
// carries, spelled the same way, so one field never means two things in two places.
// The JSON-safe shapes are the ones a shell cannot recover on its own — an integer past
// 2^53 as decimal digits of its own sign, money as minor units with its currency and
// scale — so each is read off the list page's cells and the form page's controls and
// compared with the record page's field for the same row, and with the digits themselves.
// A kind that reached a shell as prose would refuse this case rather than pass it.
func TestTheValueDomainReadsTheSameAtEveryPageKind(t *testing.T) {
	t.Parallel()
	in := domainInput(map[string]any{
		"id": "t1", "title": "n", "count": json.Number("-9007199254740993"),
		"amount": json.Number("-9007199254740993"), "currency": "EUR", "fee": json.Number("400"),
	}, time.UTC)
	record := recordCells(t, in)
	for _, kind := range []view.Kind{view.KindList, view.KindForm} {
		t.Run(string(kind), func(t *testing.T) {
			shown := in
			shown.State.Kind = kind
			if kind == view.KindForm {
				shown.State.Form = view.FormState{Create: true, Title: "New task"}
			}
			page := build(t, shown)
			type reading struct {
				kind  view.ValueKind
				value string
			}
			got := map[string]reading{}
			switch kind {
			case view.KindList:
				for _, row := range page.Table.Rows {
					for _, cell := range row.Cells {
						got[cell.Field] = reading{cell.ValueKind, string(bytes(t, cell.Value))}
					}
				}
			case view.KindForm:
				for _, control := range page.Form.Controls {
					got[control.Name] = reading{control.ValueKind, string(bytes(t, control.Value))}
				}
			}
			for _, name := range []string{"count", "fee"} {
				r, ok := got[name]
				if !ok {
					t.Fatalf("the %s page shows no %s, so this case would inspect nothing", kind, name)
				}
				if field := record[name]; r.kind != field.ValueKind || r.value != string(bytes(t, field.Value)) {
					t.Errorf("the %s page's %s is %s %s, the record page's is %s %s",
						kind, name, r.kind, r.value, field.ValueKind, string(bytes(t, field.Value)))
				}
			}
			if count := got["count"]; count.kind != view.ValueBigInteger || count.value != `"-9007199254740993"` {
				t.Errorf("the %s page's count is %s %s, want the digits -9007199254740993 kept as a big integer", kind, count.kind, count.value)
			}
			var fee struct {
				MinorUnits string `json:"minorUnits"`
				Currency   string `json:"currency"`
				Scale      int    `json:"scale"`
			}
			if err := json.Unmarshal([]byte(got["fee"].value), &fee); err != nil {
				t.Fatalf("the %s page's fee does not spell money: %v", kind, err)
			}
			if fee.MinorUnits != "400" || fee.Currency != "EUR" || fee.Scale != 0 {
				t.Errorf("the %s page's fee is %s minor units of %q at scale %d, want 400 of \"EUR\" at scale 0",
					kind, fee.MinorUnits, fee.Currency, fee.Scale)
			}
		})
	}
}
