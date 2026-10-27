package view_test

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"
)

// TestAnAmountHandedOverAsAnIntKeepsItsDigitsAtTheEndOfItsRange reads the money of a
// caller whose amount is an `int`: a branch of the money spelling apart from the
// `int64` and decoded-number ones, asking for the ends of the range a 64-bit int holds
// and no double holds. At any smaller amount a double spells the same digits, so a case
// there would let a rewrite of this leg through a float64 ship. An int32 carries no such
// amount — every int32 is exact in a double — so no value can refuse a rewrite of that
// leg, and this case names only the leg it can actually catch.
func TestAnAmountHandedOverAsAnIntKeepsItsDigitsAtTheEndOfItsRange(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("where an int is 32 bits it carries no amount a double cannot hold")
	}
	for _, tc := range []struct {
		name   string
		amount int
		digits string
	}{
		{"greatest", int(int64(math.MaxInt64)), "9223372036854775807"},
		{"least", int(int64(math.MinInt64)), "-9223372036854775808"},
	} {
		doc, err := json.Marshal(build(t, domainInput(map[string]any{
			"id": "t1", "title": "n", "amount": tc.amount, "currency": "EUR",
		}, time.UTC)))
		if err != nil {
			t.Fatalf("the page does not marshal: %v", err)
		}
		// A page that calls nothing money would let this case pass by reading nothing.
		money := moneyMembers(t, doc)
		if len(money) == 0 {
			t.Fatal("the page's document says nothing is money, so this case would inspect nothing")
		}
		for _, member := range money {
			// The schema declares scale 2 for this field, and a shell divides the minor
			// units by 10^scale, so the scale travels with the digits.
			var spelling struct {
				MinorUnits json.RawMessage `json:"minorUnits"`
				Currency   string          `json:"currency"`
				Scale      int             `json:"scale"`
			}
			if err := json.Unmarshal(member, &spelling); err != nil {
				t.Fatalf("the page spells money as %s: %v", member, err)
			}
			if string(spelling.MinorUnits) != `"`+tc.digits+`"` || spelling.Currency != "EUR" || spelling.Scale != 2 {
				t.Errorf("the page writes the amount int/%s as minor units %s of %q at scale %d, want the digits %s of %q at scale 2",
					tc.name, spelling.MinorUnits, spelling.Currency, spelling.Scale, `"`+tc.digits+`"`, "EUR")
			}
		}
	}
}
