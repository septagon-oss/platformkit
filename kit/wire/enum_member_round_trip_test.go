package wire_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// readBackMembers are member sets no two of which may share a spelling: each holds a
// member the others do not, and several hold a member the old joins-and-splits reading
// turned into two members, into none, or into the keyword that followed the enum.
func readBackMembers() [][]any {
	return [][]any{
		{"a"},
		{"a|b"},
		{"a", "b"},
		{"a b"},
		{""},
		{"", "a"},
		{"|"},
		{`a"b`},
		{`a\b`},
		{"a\tb"},
		{1, 2},
		{"1"},
		{true},
		{"true"},
		{`enum=x`},
	}
}

// sameMembers are pairs of writings that are one contract: a set is a set, and a member is
// its value rather than the digits or the order the document happened to use.
func sameMembers() [][2][]any {
	return [][2][]any{
		{{"a", "a"}, {"a"}},
		{{"b", "a"}, {"a", "b"}},
		{{json.Number("1"), 2}, {json.Number("1.0"), json.Number("2e0")}},
		{{"", ""}, {""}},
		{{json.Number("0.10"), json.Number("-0")}, {json.Number("1e-1"), 0}},
	}
}

// TestEnumMemberSpellingReadsBackItsMembers is the round trip: every pair of distinct sets
// is a change the rules refuse, in either direction, and every pair of writings that means
// one set is not a change at all. A spelling that collapsed two sets into one text, or one
// set into two, fails here whichever direction the document moved.
func TestEnumMemberSpellingReadsBackItsMembers(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		sets := readBackMembers()
		for _, direction := range []string{"request", "response"} {
			_, key := shapeAddress(kind, direction)
			for first := range sets {
				for second := first + 1; second < len(sets); second++ {
					t.Run(fmt.Sprintf("%s/%s/distinct/%d/%d", kind, direction, first, second), func(t *testing.T) {
						old := declaringMembers(t, kind, direction, sets[first])
						fresh := declaringMembers(t, kind, direction, sets[second])
						if breaks := wire.Compare(old, fresh); len(breaks) != 1 || breaks[0].Member != key || (breaks[0].Rule != "B3" && breaks[0].Rule != "B5") {
							t.Fatalf("%v vs %v: %#v", sets[first], sets[second], breaks)
						}
						if back := wire.Compare(fresh, old); len(back) != 1 || back[0].Member != key {
							t.Fatalf("%v vs %v backwards: %#v", sets[second], sets[first], back)
						}
					})
				}
			}
		}
		for _, pair := range sameMembers() {
			t.Run(kind+"/same", func(t *testing.T) {
				old := declaringMembers(t, kind, "request", pair[0])
				fresh := declaringMembers(t, kind, "request", pair[1])
				if breaks := wire.Compare(old, fresh); len(breaks) != 0 {
					t.Fatalf("%v and %v are one set: %#v", pair[0], pair[1], breaks)
				}
			})
		}
	}
}

// TestAKeywordBesideAnEnumIsNotReadAsAMember asks a change no enum took part in to name the
// bound that moved, rather than the bound itself as a member that vanished. The shape also
// carries `nullable`, which the signature spells as a bare word after the enum.
func TestAKeywordBesideAnEnumIsNotReadAsAMember(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range []struct{ direction, property string }{{"request", "kind"}, {"response", "name"}} {
			t.Run(kind+"/"+tc.direction, func(t *testing.T) {
				bounded := func(maxLength int) []byte {
					return mutated(t, baseline(kind), func(doc map[string]any) {
						at(shape(doc, kind, tc.direction), "properties")[tc.property] = map[string]any{
							"type": "string", "enum": []any{"a"}, "maxLength": maxLength, "nullable": true,
						}
					})
				}
				path, key := shapeAddress(kind, tc.direction)
				want := wire.Break{Rule: "B3", Path: path, Member: key, Message: fmt.Sprintf(
					`B3 (breaking): %s changed from "type=string enum=a maxLength=5 nullable" to "type=string enum=a maxLength=6 nullable"`, key)}
				if got := wire.Compare(bounded(5), bounded(6)); len(got) != 1 || got[0] != want {
					t.Fatalf("got %#v; want %#v", got, want)
				}
			})
		}
	}
}
