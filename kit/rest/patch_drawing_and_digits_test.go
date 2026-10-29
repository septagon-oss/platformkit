package rest

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// A patch body where a drawing is declared is read field by field: the drawing through its exact
// tuple codec, every other field through the number handling of the request door. Both codecs run
// over one body, so a body that carries both must keep both halves of itself.
func TestADrawingAndAnOrdinaryNumberBeyondFloat64InOnePatchBodyKeepTheirOwnDigits(t *testing.T) {
	type input struct {
		AmountCents int64         `json:"amountCents"`
		Drawing     entity.Sketch `json:"drawing"`
	}
	var values patchValues[input]
	body := `{"drawing":[[[0,0,0],[500,250,17]]],"amountCents":9007199254740995}`
	if err := json.Unmarshal([]byte(body), &values); err != nil {
		t.Fatalf("body refused: %v", err)
	}
	amount, ok := values["amountCents"].(json.Number)
	if !ok || amount.String() != "9007199254740995" {
		t.Fatalf("amountCents = %#v, want the exact token 9007199254740995 the body wrote", values["amountCents"])
	}
	drawing, ok := values["drawing"].(entity.Sketch)
	if !ok {
		t.Fatalf("drawing = %#v, want a decoded drawing", values["drawing"])
	}
	if want := (entity.Sketch{{{0, 0, 0}, {500, 250, 17}}}); !reflect.DeepEqual(drawing, want) {
		t.Fatalf("drawing = %v, want %v", drawing, want)
	}
}
