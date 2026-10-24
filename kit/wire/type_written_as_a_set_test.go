package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// A type written as a set is the spelling a nullable field gets, and it says which values
// answer. One name and a set of one are one contract; a set that gained or lost a name is
// another, in either direction, because this gate takes no position on whether a widened
// answer is safe for an older reader.
func TestATypeSetIsComparedAsTheSetItIs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		golden, fresh any
		from, to      string
	}{
		{"one name becomes a set of it", "array", []any{"array"}, "type=array", "type=array"},
		{"a set is written in another order", []any{"null", "array"}, []any{"array", "null"}, "type=[array|null]", "type=[array|null]"},
		{"a set is written with a repeat", []any{"array", "array"}, []any{"array"}, "type=array", "type=array"},
		{"the null answer is taken away", []any{"array", "null"}, []any{"array"}, "type=[array|null]", "type=array"},
		{"the null answer is given back", []any{"array"}, []any{"array", "null"}, "type=array", "type=[array|null]"},
		{"another answer joins", []any{"string", "null"}, []any{"string", "number", "null"}, "type=[null|string]", "type=[null|number|string]"},
		{"the set answers another type", []any{"string", "null"}, []any{"integer", "null"}, "type=[null|string]", "type=[integer|null]"},
		{"one name becomes another", "string", "integer", "type=string", "type=integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := withForm(t, func(s map[string]any) { s["type"] = tc.golden })
			fresh := withForm(t, func(s map[string]any) { s["type"] = tc.fresh })
			want := []string{}
			if tc.from != tc.to {
				want = []string{closedShape(shapeKey, tc.from, tc.to)}
			}
			if got := messagesOf(wire.Compare(old, fresh), "B3"); !slices.Equal(got, want) {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}

// A type that names nothing is not an open type: a document whose `type` is an empty set,
// an empty name or a set holding something that is not a name refuses as a profile this
// gate cannot read, rather than comparing as the "anything" it does not say.
func TestATypeThatNamesNothingRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"an empty set", []any{}},
		{"an empty name", ""},
		{"a set holding a number", []any{"string", 5}},
		{"a set holding an empty name", []any{"string", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := baseline("openapi")
			fresh := mutated(t, old, func(doc map[string]any) {
				shape(doc, "openapi", "request")["type"] = tc.value
			})
			breaks := wire.Compare(old, fresh)
			if len(breaks) != 1 || breaks[0].Rule != "B3" || !strings.Contains(breaks[0].Message, "type must name") {
				t.Fatalf("got %v; want one B3 naming the type", breaks)
			}
		})
	}
}

// A list bound is a contract about how many answers arrive, spelled by the same rule as the
// length bounds beside it.
func TestACountBoundIsCompared(t *testing.T) {
	t.Parallel()
	old := withForm(t, func(s map[string]any) { s["minItems"], s["maxItems"] = 1, 3 })
	fresh := withForm(t, func(s map[string]any) { s["minItems"], s["maxItems"] = 2, 3 })
	want := []string{closedShape(shapeKey, "type=object minItems=1 maxItems=3", "type=object minItems=2 maxItems=3")}
	if got := messagesOf(wire.Compare(old, fresh), "B3"); !slices.Equal(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	if breaks := wire.Compare(old, old); len(breaks) != 0 {
		t.Fatalf("an unchanged pair of count bounds must certify, got %v", breaks)
	}
}
