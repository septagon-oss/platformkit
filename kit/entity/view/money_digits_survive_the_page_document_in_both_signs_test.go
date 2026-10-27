package view_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestThePageDocumentKeepsMoneyDigitsInBothSigns marshals whole pages and asks, of
// every member the document says is money, for the amount's digits as a JSON string.
// A shell divides minor units by 10^scale and cannot recover a sum it cannot read, so
// the digits must survive the document and not merely the Go value, and they must
// survive in both signs: a magnitude test written as `> 2^53` rather than as magnitude
// keeps the positive amounts and loses the negative ones, and its mirror loses the
// positive. Each sign is read off a list page, a record page and a form page, so a
// place the document shows money at is never the one place nobody checked.
func TestThePageDocumentKeepsMoneyDigitsInBothSigns(t *testing.T) {
	t.Parallel()
	for _, digits := range []string{"9007199254740993", "-9007199254740993"} {
		in := domainInput(map[string]any{
			"id": "t1", "title": "n", "amount": json.Number(digits), "currency": "EUR",
		}, time.UTC)
		for _, kind := range []view.Kind{view.KindList, view.KindRecord, view.KindForm} {
			t.Run(string(kind)+"/"+digits, func(t *testing.T) {
				shown := in
				shown.State.Kind = kind
				if kind == view.KindForm {
					shown.State.Form = view.FormState{Create: true, Title: "New task"}
				}
				doc, err := json.Marshal(build(t, shown))
				if err != nil {
					t.Fatalf("the %s page does not marshal: %v", kind, err)
				}
				money := moneyMembers(t, doc)
				if len(money) == 0 {
					t.Fatalf("the %s page's document says nothing is money, so this case would inspect nothing", kind)
				}
				for _, member := range money {
					var spelling struct {
						MinorUnits json.RawMessage `json:"minorUnits"`
						Currency   string          `json:"currency"`
					}
					if err := json.Unmarshal(member, &spelling); err != nil {
						t.Fatalf("the %s page spells money as %s: %v", kind, member, err)
					}
					if string(spelling.MinorUnits) != `"`+digits+`"` || spelling.Currency != "EUR" {
						t.Errorf("the %s page writes an amount's minor units as %s of %q, want the digits %s as a JSON string in %q",
							kind, spelling.MinorUnits, spelling.Currency, `"`+digits+`"`, "EUR")
					}
				}
			})
		}
	}
}

// moneyMembers are the document's own bytes of every object that calls itself money,
// read out of the marshalled page rather than out of the Go value the builder returned.
func moneyMembers(t *testing.T, doc []byte) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	var walk func(raw json.RawMessage)
	walk = func(raw json.RawMessage) {
		trimmed := json.RawMessage(strings.TrimSpace(string(raw)))
		switch {
		case len(trimmed) == 0:
		case trimmed[0] == '[':
			var items []json.RawMessage
			if err := json.Unmarshal(trimmed, &items); err != nil {
				t.Fatalf("the document holds a list that does not decode: %v", err)
			}
			for _, item := range items {
				walk(item)
			}
		case trimmed[0] == '{':
			var members map[string]json.RawMessage
			if err := json.Unmarshal(trimmed, &members); err != nil {
				t.Fatalf("the document holds an object that does not decode: %v", err)
			}
			if kind, ok := members["valueKind"]; ok && strings.TrimSpace(string(kind)) == `"money"` {
				if value, has := members["value"]; has {
					out = append(out, value)
				}
			}
			for _, member := range members {
				walk(member)
			}
		}
	}
	walk(doc)
	return out
}
