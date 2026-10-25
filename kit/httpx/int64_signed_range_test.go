package httpx_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// rangeBody carries an int64 with no declared bounds, so a request may name any
// value the type holds and the probe echoes it back. What the wire must never do
// is round a value it accepted.
type rangeBody struct {
	Amount int64 `json:"amount"`
}

type rangeReply rangeBody

// TestTheServedIntegerFormsCarryTheFullSignedRangeInExactDigits posts the signed
// int64 limits and both signs just past float64's exact integer range, in both
// accepted spellings, and reads them back in each negotiated representation: the
// numeric response prints every digit, the decimal-string response quotes them. A
// value past either end is refused before the handler runs, spelled either way.
func TestTheServedIntegerFormsCarryTheFullSignedRangeInExactDigits(t *testing.T) {
	api, router, f := setupWith(t, true)
	f.allow = true
	f.signedIn()
	calls := new(atomic.Int32)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "signed-range-integer", Method: http.MethodPost, Path: "/signed-range",
	}, httpx.Permission("probe:write"), func(_ context.Context, in *struct{ Body rangeBody }) (*struct{ Body rangeReply }, error) {
		calls.Add(1)
		return &struct{ Body rangeReply }{rangeReply(in.Body)}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, value := range []int64{math.MinInt64, math.MaxInt64, 1<<53 + 1, -(1<<53 + 1)} {
		digits := strconv.FormatInt(value, 10)
		for _, sent := range []string{digits, `"` + digits + `"`} {
			for _, accept := range []string{"application/json", "application/vnd.platformkit.int64+json"} {
				accepted++
				want := digits
				if accept != "application/json" {
					want = `"` + digits + `"`
				}
				got := integerRequest(router, http.MethodPost, at(api, "/signed-range"), `{"amount":`+sent+`}`, accept, "application/json")
				var body map[string]json.RawMessage
				if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &body) != nil {
					t.Fatalf("POST %s (Accept %s) = %d: %s", sent, accept, got.Code, got.Body.String())
				}
				if string(body["amount"]) != want {
					t.Errorf("POST %s (Accept %s) served amount %s, want %s", sent, accept, body["amount"], want)
				}
			}
		}
	}
	for _, over := range []string{"9223372036854775808", `"9223372036854775808"`, "-9223372036854775809", `"-9223372036854775809"`} {
		got := integerRequest(router, http.MethodPost, at(api, "/signed-range"), `{"amount":`+over+`}`, "application/vnd.platformkit.int64+json", "application/json")
		if got.Code != http.StatusUnprocessableEntity {
			t.Errorf("POST %s past the signed range = %d, want 422: %s", over, got.Code, got.Body.String())
		}
	}
	if calls.Load() != int32(accepted) {
		t.Errorf("handler ran %d times, want one per accepted POST and none for a refusal", calls.Load())
	}
}
