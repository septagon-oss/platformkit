package wire_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

func TestPatternEscapesPreserveTheShapeDiagnostic(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, direction := range []string{"request", "response"} {
			for _, tc := range []struct {
				name, pattern, signature string
			}{
				{"quote", `a"b x enum=p`, `type=string pattern="a\"b x enum=p"`},
				{"backslash", `a\\b x enum=p`, `type=string pattern="a\\\\b x enum=p"`},
				{"backslash and quote", `a\\"b x enum=p`, `type=string pattern="a\\\\\"b x enum=p"`},
			} {
				t.Run(kind+"/"+direction+"/"+tc.name, func(t *testing.T) {
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
					old := with(tc.pattern)
					if breaks := wire.Compare(old, old); len(breaks) != 0 {
						t.Fatalf("identical pattern refused: %#v", breaks)
					}
					path, key := shapeAddress(kind, direction)
					want := wire.Break{Rule: "B3", Path: path, Member: key, Message: fmt.Sprintf(
						"B3 (breaking): %s changed from %q to %q", key, tc.signature, strings.Replace(tc.signature, "enum=p", "enum=q", 1))}
					fresh := with(strings.Replace(tc.pattern, "enum=p", "enum=q", 1))
					if breaks := wire.Compare(old, fresh); len(breaks) != 1 || breaks[0] != want {
						t.Fatalf("got %#v; want %#v", breaks, want)
					}
				})
			}
		}
	}
}
