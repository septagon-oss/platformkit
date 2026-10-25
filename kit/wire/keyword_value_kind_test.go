package wire_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// keywordValues are pairs of bound values whose JSON kind is what changed: the number 5 and
// the string "5" are two different promises about a field, and a bound that is an object is
// compared as the JSON it is. Each row states the diagnostic with both shape texts written
// out, quotes and escapes included.
func keywordValues() []struct {
	name, direction, keyword string
	from, to                 any
	want                     string
} {
	return []struct {
		name, direction, keyword string
		from, to                 any
		want                     string
	}{
		{"a number becomes the string of that number", "request", "maximum", json.Number("5"), "5",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=5" to "type=string enum=a|b maximum=\"5\""`},
		{"a string becomes the number it spells", "request", "pattern", "5", json.Number("5"),
			`B3 (breaking): %s changed from "type=string enum=a|b pattern=\"5\"" to "type=string enum=a|b pattern=5"`},
		{"a fraction becomes the string of that fraction", "request", "maximum", json.Number("1.5"), "1.5",
			`B3 (breaking): %s changed from "type=string enum=a|b maximum=1.5" to "type=string enum=a|b maximum=\"1.5\""`},
		{"a number becomes a bool", "request", "minimum", json.Number("5"), true,
			`B3 (breaking): %s changed from "type=string enum=a|b minimum=5" to "type=string enum=a|b minimum=true"`},
		{"an object bound is spelled as the JSON it is", "response", "maxLength", map[string]any{"a": json.Number("1")}, map[string]any{"a": json.Number("1.0")},
			`B3 (breaking): %s changed from "type=string enum=a maxLength={\"a\":1}" to "type=string enum=a maxLength={\"a\":1.0}"`},
		{"the separators an object bound holds are escaped into the token", "response", "maxLength", map[string]any{"a": "b c"}, map[string]any{"a": "b|c"},
			`B3 (breaking): %s changed from "type=string enum=a maxLength={\"a\":\"b\\u0020c\"}" to "type=string enum=a maxLength={\"a\":\"b\\u007cc\"}"`},
	}
}

// TestAKeywordValueKeepsItsJSONKind asks a bound to refuse when only the kind of its value
// moved, which is the half of the lossy normalizer the enum fix alone would have left.
func TestAKeywordValueKeepsItsJSONKind(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range keywordValues() {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				property := "name"
				if tc.direction == "request" {
					property = "kind"
				}
				setting := func(value any) []byte {
					return mutated(t, baseline(kind), func(doc map[string]any) {
						at(shape(doc, kind, tc.direction), "properties", property)[tc.keyword] = value
					})
				}
				path, key := shapeAddress(kind, tc.direction)
				want := wire.Break{Rule: "B3", Path: path, Member: key, Message: fmt.Sprintf(tc.want, key)}
				if got := wire.Compare(setting(tc.from), setting(tc.to)); len(got) != 1 || got[0] != want {
					t.Fatalf("got %#v; want %#v", got, want)
				}
			})
		}
	}
}
