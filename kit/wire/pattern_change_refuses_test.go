package wire_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// TestPatternChangeWithEnumWordsRefuses pins that a changed pattern whose text holds the
// words of an enum segment is still refused, whatever rule names it.
func TestPatternChangeWithEnumWordsRefuses(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, direction := range []string{"request", "response"} {
			t.Run(kind+"/"+direction, func(t *testing.T) {
				property := "name"
				if direction == "request" {
					property = "kind"
				}
				with := func(pattern string) []byte {
					return mutated(t, baseline(kind), func(doc map[string]any) {
						node := at(shape(doc, kind, direction), "properties", property)
						delete(node, "enum")
						node["pattern"] = pattern
					})
				}
				if breaks := wire.Compare(with("x enum=p"), with("x enum=q")); len(breaks) == 0 {
					t.Fatal("a changed pattern was not refused")
				}
			})
		}
	}
}
