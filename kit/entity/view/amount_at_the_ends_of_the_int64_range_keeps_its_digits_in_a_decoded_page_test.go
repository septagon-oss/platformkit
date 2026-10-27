package view_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnAmountAtTheEndsOfTheInt64RangeKeepsItsDigitsInADecodedPage reads the row off a
// JSON payload the way a read does — decoded with UseNumber, which is how the request
// document and the screens decode — and asks for the digits of the two amounts an int64
// holds and no double holds: 9223372036854775807 and -9223372036854775808. A shell
// divides minor units by 10^scale, so digits lost here are a sum lost in the shell.
// The least end is the one a magnitude guard written as a negation or a subtraction
// loses, and every amount this delivery otherwise holds sits near 9e15 rather than near
// 9.2e18, so nothing else reaches it. The expected digits are the payload's own text: no
// double stands between what was stored and what is asserted.
func TestAnAmountAtTheEndsOfTheInt64RangeKeepsItsDigitsInADecodedPage(t *testing.T) {
	t.Parallel()
	rows := decodedRows(t, `[{"id":"t1","title":"n","amount":9223372036854775807,"currency":"EUR"},`+
		`{"id":"t2","title":"m","amount":-9223372036854775808,"currency":"EUR"}]`)
	for _, tc := range []struct {
		row    map[string]any
		digits string
	}{
		{rows[0], "9223372036854775807"},
		{rows[1], "-9223372036854775808"},
	} {
		for _, kind := range []view.Kind{view.KindList, view.KindRecord, view.KindForm} {
			t.Run(string(kind)+"/"+tc.digits, func(t *testing.T) {
				in := domainInput(tc.row, time.UTC)
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
					}
					if err := json.Unmarshal(member, &spelling); err != nil {
						t.Fatalf("the %s page spells money as %s: %v", kind, member, err)
					}
					if string(spelling.MinorUnits) != `"`+tc.digits+`"` || spelling.Currency != "EUR" {
						t.Errorf("the %s page writes the decoded amount's minor units as %s of %q, want the digits %s as a JSON string in %q",
							kind, spelling.MinorUnits, spelling.Currency, `"`+tc.digits+`"`, "EUR")
					}
				}
			})
		}
	}
}

// decodedRows are the rows of a resource read as the transport hands them over: decoded
// from their JSON text with UseNumber, so a whole number arrives as json.Number rather
// than as a double, exactly as the request document and the screens decode them.
func decodedRows(t *testing.T, payload string) []map[string]any {
	t.Helper()
	reader := json.NewDecoder(strings.NewReader(payload))
	reader.UseNumber()
	var rows []map[string]any
	if err := reader.Decode(&rows); err != nil {
		t.Fatalf("a read's payload does not decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows decoded, want the two the payload holds", len(rows))
	}
	return rows
}
