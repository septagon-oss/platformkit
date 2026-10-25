package wire_test

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// TestPatternWordsAreNotReadAsAnEnumSegment asks a change of one pattern whose text holds
// the words of an enum segment: the rule that names it is B3, which prints the pattern it
// was and the pattern it became. A scan that read `enum=` out of a quoted keyword value
// refuses the same change as B5 and names a member no document declares, which says the
// wrong thing about a change that is refused either way.
func TestPatternWordsAreNotReadAsAnEnumSegment(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, direction := range []string{"request", "response"} {
			t.Run(kind+"/"+direction, func(t *testing.T) {
				property := "name"
				if direction == "request" {
					property = "kind"
				}
				path, key := shapeAddress(kind, direction)
				with := func(pattern string) []byte {
					return mutated(t, baseline(kind), func(doc map[string]any) {
						node := at(shape(doc, kind, direction), "properties", property)
						delete(node, "enum")
						node["pattern"] = pattern
					})
				}
				want := wire.Break{Rule: "B3", Path: path, Member: key, Message: fmt.Sprintf(
					`B3 (breaking): %s changed from "type=string pattern=\"x enum=p\"" to "type=string pattern=\"x enum=q\""`, key)}
				if breaks := wire.Compare(with("x enum=p"), with("x enum=q")); len(breaks) != 1 || breaks[0] != want {
					t.Fatalf("got %#v; want %#v", breaks, want)
				}
			})
		}
	}
}
