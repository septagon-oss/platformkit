package wire_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// equivalentSpellings are writings of one number: the same figure plain, with a fraction,
// with an exponent, with a leading zero, with a negative zero. A gate that refused these
// would refuse a re-render rather than a change of contract, which is the mistake the exact
// digits must not be bought with.
func equivalentSpellings() [][2]string {
	return [][2]string{
		{"1", "1.0"},
		{"1", "1e0"},
		{"1.0", "1E0"},
		{"0.1", "1e-1"},
		{"1000", "1e3"},
		{"1000", "1.0e3"},
		{"1.50", "1.5"},
		{"120", "1.2e2"},
		{"-0", "0"},
		{"0.000", "-0.00"},
		{"0.0001", "1e-4"},
		{"2.5e-3", "0.0025"},
		{"9007199254740993", "9007199254740993.0"},
		{"9007199254740992", "9.007199254740992e15"},
	}
}

// TestEquivalentNumberSpellingsStayNonbreaking runs each pair over every bound the shape
// signature spells, on the request side and the answer side of both document kinds. The
// other half of the rule — that a different number never disappears — is the exact-bound cases.
func TestEquivalentNumberSpellingsStayNonbreaking(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, direction := range []string{"request", "response"} {
			for _, keyword := range []string{"minLength", "maxLength", "minimum", "maximum"} {
				for _, pair := range equivalentSpellings() {
					t.Run(fmt.Sprintf("%s/%s/%s/%s=%s", kind, direction, keyword, pair[0], pair[1]), func(t *testing.T) {
						old := bounding(t, kind, direction, keyword, json.Number(pair[0]))
						fresh := bounding(t, kind, direction, keyword, json.Number(pair[1]))
						if got := wire.Compare(old, fresh); len(got) != 0 {
							t.Fatalf("%s %s=%s and %s=%s are one bound: %#v", direction, keyword, pair[0], keyword, pair[1], got)
						}
					})
				}
			}
		}
	}
}

// TestADeclarationNumberKeepsItsValueAcrossSpellings asks the same rule of a number inside
// an authorization declaration, which is compared as the whole declaration: a door written
// `1` is the door written `1.0`, and a door whose figure moved past what a float64 could
// hold is a different door.
func TestADeclarationNumberKeepsItsValueAcrossSpellings(t *testing.T) {
	declaring := func(value json.Number) []byte {
		return mutated(t, baseline("openapi"), func(doc map[string]any) {
			operation(doc, "openapi")["x-platformkit-auth"] = map[string]any{"kind": value}
		})
	}
	for _, pair := range equivalentSpellings() {
		t.Run(pair[0]+"="+pair[1], func(t *testing.T) {
			if got := wire.Compare(declaring(json.Number(pair[0])), declaring(json.Number(pair[1]))); len(got) != 0 {
				t.Fatalf("kind=%s and kind=%s are one declaration: %#v", pair[0], pair[1], got)
			}
		})
	}
	want := wire.Break{Rule: "B6", Path: "GET /items", Member: "x-platformkit-auth",
		Message: "B6 (breaking): GET /items (list) is authorized kind=9007199254740992 where it was kind=9007199254740993"}
	if got := wire.Compare(declaring("9007199254740993"), declaring("9007199254740992")); len(got) != 1 || got[0] != want {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
