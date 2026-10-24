package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// The profile is a closed list: a keyword the comparison records or descends is read, a
// keyword that only names or documents a schema is ignored on purpose, and anything else
// inside a schema the walk reached leaves the comparison incomplete. A keyword nobody reads
// is not a keyword that says nothing.
func TestAnUnreadConstraintRefusesAsIncomplete(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{
		"allOf", "oneOf", "not", "if", "then", "else", "const", "prefixItems", "contains",
		"patternProperties", "propertyNames", "dependentRequired", "dependentSchemas",
		"unevaluatedItems", "unevaluatedProperties", "multipleOf", "exclusiveMinimum",
		"exclusiveMaximum", "uniqueItems", "minProperties", "maxProperties",
	} {
		t.Run(keyword, func(t *testing.T) {
			old := baseline("openapi")
			fresh := withForm(t, func(s map[string]any) {
				s[keyword] = []any{map[string]any{"type": "string"}}
			})
			breaks := wire.Compare(old, fresh)
			if len(breaks) != 1 || breaks[0].Rule != "B7" {
				t.Fatalf("got %v; want one incomplete refusal naming %s", breaks, keyword)
			}
			if breaks[0].Member != shapeKey || !strings.Contains(breaks[0].Message, "declares "+keyword) {
				t.Fatalf("got %v; want the refusal to name the keyword and the address", breaks)
			}
			if !strings.Contains(breaks[0].Message, "in the current document") {
				t.Fatalf("got %v; want the refusal to name the side it read", breaks)
			}
		})
	}
}

// Several unread keywords on one node are one shape the comparison could not read, named in
// one refusal that lists them all.
func TestEveryUnreadKeywordOnANodeIsNamedOnce(t *testing.T) {
	t.Parallel()
	old := baseline("openapi")
	fresh := withForm(t, func(s map[string]any) { s["minProperties"], s["uniqueItems"] = 1, true })
	breaks := wire.Compare(old, fresh)
	if len(breaks) != 1 {
		t.Fatalf("got %v; want the node named once, whatever it declares", breaks)
	}
	if !strings.Contains(breaks[0].Message, "declares minProperties, uniqueItems") {
		t.Fatalf("got %v; want both keywords named on the current side", breaks)
	}
}

// A key that only names, documents, samples or frames a schema says nothing about which
// instances satisfy it, so ignoring it cannot misread a contract — and a product's own
// extension key is ignored the same way. None of them makes a pair incomplete.
func TestDescriptiveKeysSayNothingAboutTheShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		keyword string
		value   any
	}{
		{"title", "the item"}, {"description", "what it is"}, {"$comment", "why"},
		{"$anchor", "here"}, {"$id", "urn:item"},
		{"$schema", "https://json-schema.org/draft/2020-12/schema"},
		{"default", map[string]any{}}, {"deprecated", true}, {"example", map[string]any{"kind": "a"}},
		{"examples", []any{map[string]any{"kind": "a"}}}, {"readOnly", true}, {"writeOnly", false},
		{"contentEncoding", "base64"}, {"contentMediaType", "application/json"},
		{"discriminator", map[string]any{"propertyName": "kind"}},
		{"xml", map[string]any{"name": "item"}},
		{"externalDocs", map[string]any{"url": "https://platformkit.test"}},
		{"x-internal", true},
		// A definitions block is descriptive where the walk never reaches it: nothing in it
		// constrains an instance until a reference asks for it.
		{"$defs", map[string]any{"Unused": map[string]any{"uniqueItems": true}}},
	} {
		t.Run(tc.keyword, func(t *testing.T) {
			old := baseline("openapi")
			fresh := mutated(t, old, func(doc map[string]any) {
				shape(doc, "openapi", "request")[tc.keyword] = tc.value
			})
			if breaks := wire.Compare(old, fresh); len(breaks) != 0 {
				t.Fatalf("got %v; want %s ignored", breaks, tc.keyword)
			}
		})
	}
}

// An extension is descriptive by its prefix, so a product can carry its own key without the
// comparison claiming to have read it.
func TestAnExtensionKeyOfAnyNameIsIgnored(t *testing.T) {
	t.Parallel()
	old := baseline("openapi")
	fresh := mutated(t, old, func(doc map[string]any) {
		shape(doc, "openapi", "request")["x-platformkit-column"] = "kind"
	})
	if breaks := wire.Compare(old, fresh); len(breaks) != 0 {
		t.Fatalf("got %v; want the extension ignored", breaks)
	}
}

// The event document reaches the same traversal as the request body, so an in-profile shape
// it cannot read refuses the same way.
func TestAnUnreadConstraintRefusesAnEventPayloadToo(t *testing.T) {
	t.Parallel()
	old := baseline("asyncapi")
	fresh := mutated(t, old, func(doc map[string]any) {
		shape(doc, "asyncapi", "request")["oneOf"] = []any{map[string]any{"type": "string"}}
	})
	breaks := wire.Compare(old, fresh)
	if len(breaks) != 1 || breaks[0].Rule != "B7" || !strings.Contains(breaks[0].Message, "oneOf") {
		t.Fatalf("got %v; want one incomplete refusal over the event payload", breaks)
	}
	if breaks[0].Path != "RECEIVE item.create" {
		t.Fatalf("got %q; want the operation that announced the payload", breaks[0].Path)
	}
}

// An allowance is a reviewed pair of declarations. It covers the one rule that compares a
// pair of authorizations and can never certify a shape the comparison never read.
func TestNoAllowanceCoversAnUnfinishedComparison(t *testing.T) {
	t.Parallel()
	old := baseline("openapi")
	fresh := withForm(t, func(s map[string]any) { s["allOf"] = []any{map[string]any{"type": "string"}} })
	breaks := wire.CompareWithAllowances(old, fresh, allowance())
	if !slices.ContainsFunc(breaks, func(b wire.Break) bool { return b.Rule == "B7" }) {
		t.Fatalf("got %v; want the incomplete refusal to survive an allowance", breaks)
	}
}
