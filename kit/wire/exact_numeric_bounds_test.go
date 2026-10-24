package wire_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// bounding is the baseline with one bound written on one side's property. The document
// keeps the spelling it was given: a number reaches the comparator as the literal, never as
// the float64 a plain decode would have made of it.
func bounding(t *testing.T, kind, direction, keyword string, value json.Number) []byte {
	t.Helper()
	property := "name"
	if direction == "request" {
		property = "kind"
	}
	return mutated(t, baseline(kind), func(doc map[string]any) {
		at(shape(doc, kind, direction), "properties", property)[keyword] = value
	})
}

// exactBounds are pairs of bounds no float64 round trip can tell apart, or tell apart only
// by luck, and the one diagnostic each change earns with both shape texts written out.
func exactBounds() []struct {
	name, direction, from, to, want string
} {
	return []struct {
		name, direction, from, to, want string
	}{
		{"one above 2^53 becomes 2^53", "request", "9007199254740993", "9007199254740992",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=9007199254740993" to "type=string enum=a|b maximum=9007199254740992"`},
		{"one above 2^53 becomes two above it", "request", "9007199254740993", "9007199254740995",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=9007199254740993" to "type=string enum=a|b maximum=9007199254740995"`},
		{"2^53 written with a fraction becomes one above it", "request", "9007199254740992.0", "9007199254740993",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=9007199254740992" to "type=string enum=a|b maximum=9007199254740993"`},
		{"a fraction past a float64's last bit is a different bound", "request", "0.10000000000000001", "0.1",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=0.10000000000000001" to "type=string enum=a|b maximum=0.1"`},
		{"a bound past the exact window is compared as written", "request", "1e999999999", "1e999999998",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum==1e999999999" to "type=string enum=a|b maximum==1e999999998"`},
		{"one above 2^53 becomes 2^53 in an answer", "response", "9007199254740993", "9007199254740992",
			`B3 (breaking): %s changed from "type=string enum=a maximum=9007199254740993" to "type=string enum=a maximum=9007199254740992"`},
	}
}

// TestBoundsBeyondFloat64Refuse asks a bound that only the exact digits can tell apart to
// refuse, on the request side, on the answer side, and through the AsyncAPI traversal that
// shares the walker with OpenAPI.
func TestBoundsBeyondFloat64Refuse(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range exactBounds() {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				path, key := shapeAddress(kind, tc.direction)
				old := bounding(t, kind, tc.direction, "maximum", json.Number(tc.from))
				fresh := bounding(t, kind, tc.direction, "maximum", json.Number(tc.to))
				want := wire.Break{Rule: "B3", Path: path, Member: key, Message: fmt.Sprintf(tc.want, key)}
				if got := wire.Compare(old, fresh); len(got) != 1 || got[0] != want {
					t.Fatalf("got %#v; want %#v", got, want)
				}
			})
		}
	}
}

// TestGoldenRefusesABoundBeyondFloat64BeforeWriting is the same mutation against the golden
// lifecycle: the flag that regenerates a stale baseline does not launder a bound an older
// client was promised, and the file keeps the digits it holds.
func TestGoldenRefusesABoundBeyondFloat64BeforeWriting(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		_, key := shapeAddress(kind, "request")
		old := bounding(t, kind, "request", "maximum", json.Number("9007199254740993"))
		fresh := bounding(t, kind, "request", "maximum", json.Number("9007199254740992"))
		output, onDisk, err := runGolden(t, old, fresh, "1", "")
		want := "B3 (breaking): " + key + ` changed from "type=string enum=a|b maximum=9007199254740993" to "type=string enum=a|b maximum=9007199254740992"`
		if err == nil || !strings.Contains(output, want) || !strings.Contains(output, "is left as it was") || !bytes.Equal(onDisk, old) {
			t.Fatalf("%s: err=%v; unchanged=%v\n%s", kind, err, bytes.Equal(onDisk, old), output)
		}
	}
}
