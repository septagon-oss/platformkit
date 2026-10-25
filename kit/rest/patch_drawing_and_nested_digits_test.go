package rest

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// A patch body's fields are decoded in map order, so what one field becomes cannot depend on
// which field the body wrote first. And the digits an ordinary number keeps are the ones the
// body wrote at every depth of that field: money nested in an object is the same money as
// money at its top, and a value that arrives as a float64 asks a caller who sent exact digits
// to send them again.
func TestAPatchBodyKeepsItsDrawingAndItsDigitsAtEveryDepthInWhateverOrderTheBodyWroteThem(t *testing.T) {
	type input struct {
		Meta    map[string]any `json:"meta"`
		Drawing entity.Sketch  `json:"drawing"`
	}
	const drawing = `[[[0,0,0],[500,250,17]]]`
	for _, body := range []string{`{"drawing":` + drawing + `,"meta":{"cents":9007199254740995,"spend":{"cents":9007199254740993}}}`,
		`{"meta":{"cents":9007199254740995,"spend":{"cents":9007199254740993}},"drawing":` + drawing + `}`} {
		var values patchValues[input]
		if err := json.Unmarshal([]byte(body), &values); err != nil {
			t.Fatalf("body refused: %v", err)
		}
		if got, ok := values["drawing"].(entity.Sketch); !ok || !reflect.DeepEqual(got, entity.Sketch{{{0, 0, 0}, {500, 250, 17}}}) {
			t.Fatalf("drawing = %#v, want the decoded drawing the body wrote", values["drawing"])
		}
		meta, ok := values["meta"].(map[string]any)
		if !ok {
			t.Fatalf("meta = %#v, want the object the body wrote", values["meta"])
		}
		if got, want := meta["cents"], "9007199254740995"; !exactToken(got, want) {
			t.Fatalf("meta.cents = %#v, want the exact token %s the body wrote", got, want)
		}
		spend, ok := meta["spend"].(map[string]any)
		if !ok {
			t.Fatalf("meta.spend = %#v, want the object the body wrote", meta["spend"])
		}
		if got, want := spend["cents"], "9007199254740993"; !exactToken(got, want) {
			t.Fatalf("meta.spend.cents = %#v, want the exact token %s the body wrote", got, want)
		}
	}
}

func exactToken(got any, want string) bool {
	number, ok := got.(json.Number)
	return ok && number.String() == want
}
