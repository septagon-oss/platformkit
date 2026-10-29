package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// shapeKey is the record of the request body's own schema node, and shapeSlot the record
// of what may sit in the properties that node does not name.
const (
	shapeKey  = "create request body application/json"
	shapeSlot = shapeKey + " {}"
)

// withForm returns baseline("openapi") with the request body schema rewritten.
func withForm(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	return mutated(t, baseline("openapi"), func(doc map[string]any) {
		if change != nil {
			change(shape(doc, "openapi", "request"))
		}
	})
}

func closedShape(key, from, to string) string {
	return "B3 (breaking): " + key + ` changed from "` + from + `" to "` + to + `"`
}

func lostShape(key string) string { return "B3 (breaking): " + key + " is gone" }

// An open schema and a closed one are different contracts, so every move between them is a
// changed shape. `true` and an empty schema say the same thing and are spelled the same
// way; `false` is the one state that says something the node's other parts do not, so it
// alone joins the node's signature.
func TestEveryMoveBetweenOpenAndClosedRefuses(t *testing.T) {
	t.Parallel()
	open, closed := "type=object", "type=object additionalProperties=false"
	for _, tc := range []struct {
		name          string
		golden, fresh func(map[string]any)
		want          []string
	}{
		{name: "silent closes", golden: nil, fresh: func(s map[string]any) { s["additionalProperties"] = false },
			want: []string{closedShape(shapeKey, open, closed)}},
		{name: "closed goes silent", golden: func(s map[string]any) { s["additionalProperties"] = false }, fresh: nil,
			want: []string{closedShape(shapeKey, closed, open)}},
		{name: "open boolean closes", golden: func(s map[string]any) { s["additionalProperties"] = true },
			fresh: func(s map[string]any) { s["additionalProperties"] = false },
			want:  []string{closedShape(shapeKey, open, closed), lostShape(shapeSlot)}},
		{name: "closed opens as a boolean", golden: func(s map[string]any) { s["additionalProperties"] = false },
			fresh: func(s map[string]any) { s["additionalProperties"] = true },
			want:  []string{closedShape(shapeKey, closed, open)}},
		{name: "silent opens as a boolean", golden: nil, fresh: func(s map[string]any) { s["additionalProperties"] = true },
			want: nil},
		{name: "an open boolean goes silent", golden: func(s map[string]any) { s["additionalProperties"] = true }, fresh: nil,
			want: []string{lostShape(shapeSlot)}},
		{name: "an empty schema and an open boolean are one contract",
			golden: func(s map[string]any) { s["additionalProperties"] = map[string]any{} },
			fresh:  func(s map[string]any) { s["additionalProperties"] = true }, want: nil},
		{name: "an open boolean becomes an empty schema", golden: func(s map[string]any) { s["additionalProperties"] = true },
			fresh: func(s map[string]any) { s["additionalProperties"] = map[string]any{} }, want: nil},
		{name: "silent becomes an empty schema", golden: nil,
			fresh: func(s map[string]any) { s["additionalProperties"] = map[string]any{} }, want: nil},
		{name: "an empty schema goes silent", golden: func(s map[string]any) { s["additionalProperties"] = map[string]any{} }, fresh: nil,
			want: []string{lostShape(shapeSlot)}},
		{name: "closed opens as a schema", golden: func(s map[string]any) { s["additionalProperties"] = false },
			fresh: func(s map[string]any) { s["additionalProperties"] = map[string]any{"type": "string"} },
			want:  []string{closedShape(shapeKey, closed, open)}},
		{name: "a schema closes the slot", golden: func(s map[string]any) { s["additionalProperties"] = map[string]any{"type": "string"} },
			fresh: func(s map[string]any) { s["additionalProperties"] = false },
			want:  []string{closedShape(shapeKey, open, closed), lostShape(shapeSlot)}},
		{name: "a value the slot allows widens", golden: func(s map[string]any) { s["additionalProperties"] = map[string]any{} },
			fresh: func(s map[string]any) { s["additionalProperties"] = map[string]any{"type": "string"} },
			want:  []string{closedShape(shapeSlot, "", "type=string")}},
		{name: "a value the slot allows narrows", golden: func(s map[string]any) { s["additionalProperties"] = map[string]any{"type": "string"} },
			fresh: func(s map[string]any) { s["additionalProperties"] = map[string]any{} },
			want:  []string{closedShape(shapeSlot, "type=string", "")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			breaks := wire.Compare(withForm(t, tc.golden), withForm(t, tc.fresh))
			if got := messagesOf(breaks, "B3"); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v; want %v", breaks, tc.want)
			}
		})
	}
}

func messagesOf(breaks []wire.Break, rules ...string) []string {
	var out []string
	for _, b := range breaks {
		if !slices.Contains(rules, b.Rule) {
			panic("unexpected rule " + b.Rule + ": " + b.Message)
		}
		out = append(out, b.Message)
	}
	return out
}

// An additionalProperties that is neither a boolean nor a schema says nothing this gate can
// read, so the document refuses rather than comparing as though the slot were open.
func TestAnAdditionalPropertiesOfAnotherKindRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"a name", "yes"}, {"a number", 1}, {"a null", nil}, {"a list", []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := baseline("openapi")
			fresh := mutated(t, old, func(doc map[string]any) {
				shape(doc, "openapi", "request")["additionalProperties"] = tc.value
			})
			breaks := wire.Compare(old, fresh)
			if len(breaks) != 1 || breaks[0].Rule != "B3" || !strings.Contains(breaks[0].Message, "additionalProperties must be a boolean or a schema") {
				t.Fatalf("got %v; want one B3 naming the slot's kind", breaks)
			}
		})
	}
}
