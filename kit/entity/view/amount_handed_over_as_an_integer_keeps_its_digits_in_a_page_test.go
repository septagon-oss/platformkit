package view_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnAmountHandedOverAsAnIntegerKeepsItsDigitsInAPage reads the money of a caller
// that hands the builder its own Go value rather than a decoded document — what a
// terminal or desktop shell written in Go does, and the reason one contract serves
// every shell. amountOf spells int, int32 and int64 apart from the json.Number a read
// carries, so an amount that arrives as one of those three reaches a shell through a
// branch nothing else in this package asks anything of: every row this delivery holds
// spells its money as a json.Number. The digits asked for are the two the range holds
// and no double holds, plus one small amount of each sign, so a spelling that survives
// a float64 at 1250 still refuses at 9223372036854775807.
func TestAnAmountHandedOverAsAnIntegerKeepsItsDigitsInAPage(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		amount any
		digits string
	}{
		{"int64/greatest", int64(math.MaxInt64), "9223372036854775807"},
		{"int64/least", int64(math.MinInt64), "-9223372036854775808"},
		{"int", int(1250), "1250"},
		{"int32", int32(-50), "-50"},
	}
	for _, tc := range rows {
		for _, kind := range []view.Kind{view.KindList, view.KindRecord, view.KindForm} {
			t.Run(tc.name+"/"+string(kind), func(t *testing.T) {
				in := domainInput(map[string]any{
					"id": "t1", "title": "n", "amount": tc.amount, "currency": "EUR",
				}, time.UTC)
				in.State.Kind = kind
				if kind == view.KindForm {
					in.State.Form = view.FormState{Create: true, Title: "New task"}
				}
				doc, err := json.Marshal(build(t, in))
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
						Scale      int             `json:"scale"`
					}
					if err := json.Unmarshal(member, &spelling); err != nil {
						t.Fatalf("the %s page spells money as %s: %v", kind, member, err)
					}
					// The task schema declares scale 2 for this field: a shell divides
					// the minor units by 10^scale, so the scale travels with the digits.
					if string(spelling.MinorUnits) != `"`+tc.digits+`"` || spelling.Currency != "EUR" || spelling.Scale != 2 {
						t.Errorf("the %s page writes the amount %s as minor units %s of %q at scale %d, want the digits %s of %q at scale 2",
							kind, tc.name, spelling.MinorUnits, spelling.Currency, spelling.Scale, `"`+tc.digits+`"`, "EUR")
					}
				}
			})
		}
	}
}
