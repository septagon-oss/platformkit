package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// wholeBodyKey is the record a request body's own requirement takes, beside the body's
// content records, spelled so the rule that refuses a new demand reads it.
const wholeBodyKey = "create request body requires body"

// withWholeBody returns baseline("openapi") with the request body object rewritten.
func withWholeBody(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	return mutated(t, baseline("openapi"), func(doc map[string]any) {
		if change != nil {
			change(at(doc, "paths", "/items", "post", "requestBody"))
		}
	})
}

// A body that says `required: true` demands a document an older client may never have
// sent, which is the same statement a required parameter makes. The pair of spellings a
// document reaches it through — a list of required names on the schema and the body
// object's own boolean — must both be read.
func TestWholeBodyRequirednessIsCompared(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		golden, fresh func(map[string]any)
		rule, member  string
		message       string
	}{
		{"an optional body becomes required", nil, func(body map[string]any) { body["required"] = true },
			"B4", wholeBodyKey, "B4 (breaking): " + wholeBodyKey + " is asked for and an older client never sends it"},
		{"a required body becomes optional", func(body map[string]any) { body["required"] = true },
			func(body map[string]any) { body["required"] = false },
			"B3", wholeBodyKey, "B3 (breaking): " + wholeBodyKey + " is gone"},
		{"a required body goes silent", func(body map[string]any) { body["required"] = true }, nil,
			"B3", wholeBodyKey, "B3 (breaking): " + wholeBodyKey + " is gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := withWholeBody(t, tc.golden)
			fresh := withWholeBody(t, tc.fresh)
			breaks := wire.Compare(old, fresh)
			want := wire.Break{Rule: tc.rule, Path: "POST /items", Member: tc.member, Message: tc.message}
			if !slices.Contains(breaks, want) {
				t.Fatalf("got %v; want %#v", breaks, want)
			}
			if len(breaks) != 1 {
				t.Fatalf("the whole-body requirement names one change, got %v", breaks)
			}
		})
	}
}

// `required: false`, `required: null` and an absent `required` are one state, which is
// what the emitter's `omitempty` writes: a document that stops saying the word has not
// relaxed anything, so no record moves and nothing is refused.
func TestAnOptionalBodySaysNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		fresh func(map[string]any)
	}{
		{"optional to silent", func(body map[string]any) { body["required"] = false }},
		{"silent to optional", func(body map[string]any) { body["required"] = false }},
		{"silent to null", func(body map[string]any) { body["required"] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := withWholeBody(t, func(body map[string]any) { body["description"] = "the same body" })
			fresh := withWholeBody(t, tc.fresh)
			if breaks := wire.Compare(old, fresh); len(breaks) != 0 {
				t.Fatalf("got %v; want the two bodies read as one contract", breaks)
			}
		})
	}
}

// A requirement nobody can read is not a requirement nobody wrote: a body or a schema that
// spells one in a kind this gate cannot name refuses as an invalid profile rather than
// comparing as though the requirement had been dropped.
func TestAMalformedRequirementRefusesAsAnInvalidProfile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, want string
		change     func(map[string]any)
	}{
		{"a body requirement that is not a boolean", "request body required must be a boolean", func(doc map[string]any) {
			at(doc, "paths", "/items", "post", "requestBody")["required"] = "yes"
		}},
		{"a schema requirement that is not a list", "required must be a list of property names", func(doc map[string]any) {
			shape(doc, "openapi", "request")["required"] = "kind"
		}},
		{"a schema requirement holding a name that is none", "required must be a list of property names", func(doc map[string]any) {
			shape(doc, "openapi", "request")["required"] = []any{"", 7}
		}},
		{"a parameter requirement that is not a boolean", "parameter required must be a boolean", func(doc map[string]any) {
			at(doc, "paths", "/items", "get")["parameters"] = []any{map[string]any{"name": "q", "in": "query", "required": "yes"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := baseline("openapi")
			breaks := wire.Compare(old, mutated(t, old, tc.change))
			if len(breaks) != 1 || breaks[0].Rule != "B3" || !strings.Contains(breaks[0].Message, tc.want) {
				t.Fatalf("got %v; want one B3 naming %q", breaks, tc.want)
			}
		})
	}
}
