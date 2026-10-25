package jsonbody

import (
	"bytes"
	"math"
	"testing"
)

func TestInt64ExtremesSurviveEncodeThenDecodeAndInexactFloatsAreRefused(t *testing.T) {
	type money struct {
		Amount int64 `json:"amount"`
	}
	for _, n := range []int64{math.MinInt64, math.MaxInt64, 1<<53 + 1, -(1<<53 + 1)} {
		var out bytes.Buffer
		if err := Encode(&out, money{n}); err != nil {
			t.Fatal(err)
		}
		var back money
		if err := Decode(out.Bytes(), &back); err != nil || back.Amount != n {
			t.Errorf("%d came back as %d, %v from %s", n, back.Amount, err, out.String())
		}
	}
	var got money
	if err := Bind(map[string]any{"amount": float64(1 << 53)}, &got); err == nil {
		t.Errorf("a float at 2^53 was bound as %d", got.Amount)
	}
	if err := Bind(map[string]any{"amount": float64(1<<53 - 1)}, &got); err != nil || got.Amount != 1<<53-1 {
		t.Errorf("an exact float was refused or changed: %d, %v", got.Amount, err)
	}
}
