package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// A schema that names itself is how a tree or a linked list is published, so the walk has to
// live with it. It expands a reference once per path — a reference already expanded above the
// node it stands on is the repeat the walk refuses — so every named schema it reaches is read
// at least once, and a cycle of itself is never a change.
func TestAnUnchangedRecursiveDocumentTerminatesAsCompatible(t *testing.T) {
	t.Parallel()
	components := map[string]any{
		"Node": map[string]any{"type": "object", "properties": map[string]any{
			"leaf": leafOfType("string"),
			"next": map[string]any{"$ref": "#/components/schemas/Node"},
		}},
		"Ring": map[string]any{"type": "object", "properties": map[string]any{
			"again": map[string]any{"$ref": "#/components/schemas/Ring"},
		}},
		"Pair": map[string]any{"type": "object", "properties": map[string]any{
			"a": map[string]any{"$ref": "#/components/schemas/Leaf"},
			"b": map[string]any{"$ref": "#/components/schemas/Leaf"},
		}},
		"Leaf": map[string]any{"type": "object", "properties": map[string]any{
			"leaf": leafOfType("integer"),
		}},
		"Mutual": map[string]any{"type": "object", "properties": map[string]any{
			"other": map[string]any{"$ref": "#/components/schemas/Other"},
		}},
		"Other": map[string]any{"type": "object", "properties": map[string]any{
			"back":    map[string]any{"$ref": "#/components/schemas/Mutual"},
			"leaf":    leafOfType("string"),
			"sibling": map[string]any{"$ref": "#/components/schemas/Node"},
		}},
	}
	for _, tc := range []struct {
		name   string
		schema map[string]any
	}{
		{"a body that is the recursive schema", map[string]any{"$ref": "#/components/schemas/Node"}},
		{"a self loop with no other member", map[string]any{"$ref": "#/components/schemas/Ring"}},
		{"a mutual pair", map[string]any{"$ref": "#/components/schemas/Mutual"}},
		{"a shared schema reached by two references", map[string]any{"$ref": "#/components/schemas/Pair"}},
		{"recursion reached through a property", map[string]any{"type": "object", "properties": map[string]any{
			"root": map[string]any{"$ref": "#/components/schemas/Node"},
			"tail": map[string]any{"$ref": "#/components/schemas/Node"},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := withComponents(t, tc.schema, components)
			if breaks := wire.Compare(body, body); len(breaks) != 0 {
				t.Fatalf("a document that names itself read as a change: %v", breaks)
			}
		})
	}
}

// A leaf inside that recursion is still a leaf: the change is refused once, under the address
// the walk reached it at, and the cycle itself adds no second refusal.
func TestAChangeInsideARecursiveDocumentNamesOneAddress(t *testing.T) {
	t.Parallel()
	body := func(kind string) []byte {
		return withComponents(t, map[string]any{"$ref": "#/components/schemas/Node"}, map[string]any{
			"Node": map[string]any{"type": "object", "properties": map[string]any{
				"leaf": leafOfType(kind),
				"next": map[string]any{"$ref": "#/components/schemas/Node"},
			}},
		})
	}
	breaks := wire.Compare(body("string"), body("integer"))
	want := []string{closedShape(shapeKey+" »#/components/schemas/Node .leaf", "type=string", "type=integer")}
	if got := refusalsAsMessages(t, breaks, "B3"); !slices.Equal(got, want) {
		t.Fatalf("got %v; want %v", breaks, want)
	}
}

// One shared schema reached by two references is expanded on both paths, so one change in it
// is named at both addresses. Two rows, one defect: the rows are the paths, not the change.
func TestASharedSchemaIsNamedAtEveryPathThatReachesIt(t *testing.T) {
	t.Parallel()
	components := map[string]any{
		"Pair": map[string]any{"type": "object", "properties": map[string]any{
			"a": map[string]any{"$ref": "#/components/schemas/Leaf"},
			"b": map[string]any{"$ref": "#/components/schemas/Leaf"},
		}},
		"Leaf": map[string]any{"type": "object", "properties": map[string]any{
			"leaf": leafOfType("integer"),
		}},
	}
	old := withComponents(t, map[string]any{"$ref": "#/components/schemas/Pair"}, components)
	changed := map[string]any{
		"Pair": components["Pair"],
		"Leaf": map[string]any{"type": "object", "properties": map[string]any{"leaf": leafOfType("string")}},
	}
	fresh := withComponents(t, map[string]any{"$ref": "#/components/schemas/Pair"}, changed)
	breaks := wire.Compare(old, fresh)
	if len(breaks) != 2 {
		t.Fatalf("got %v; want the shared leaf named once per path", breaks)
	}
	for _, suffix := range []string{" .a ", " .b "} {
		if !slices.ContainsFunc(breaks, func(b wire.Break) bool {
			return b.Rule == "B3" && strings.Contains(b.Member, suffix+"»#/components/schemas/Leaf .leaf")
		}) {
			t.Fatalf("got %v; want a refusal under the %s path", breaks, suffix)
		}
	}
}

// A recursion the walk can only reach past its own limit is the case a cycle bound alone
// cannot answer: it is unfinished analysis, named as such, and not a claim that the cycle is
// a breaking change.
func TestARecursionPastTheLimitRefusesAsUnfinished(t *testing.T) {
	t.Parallel()
	deep := nestedSchemas(depthLimit-1, map[string]any{"$ref": "#/components/schemas/Node"})
	body := withComponents(t, deep, map[string]any{"Node": map[string]any{"type": "object", "properties": map[string]any{
		"leaf": leafOfType("string"),
		"next": map[string]any{"$ref": "#/components/schemas/Node"},
	}}})
	breaks := wire.Compare(body, body)
	if len(breaks) == 0 {
		t.Fatal("a recursion below the limit read as certified")
	}
	if got := refusalsAsMessages(t, breaks, "B7"); len(got) < 2 {
		t.Fatalf("got %v; want an unfinished refusal for each side", breaks)
	}
}

// withComponents is baseline("openapi") with its request body schema and the components the
// schema names.
func withComponents(t *testing.T, schema, components map[string]any) []byte {
	t.Helper()
	return mutated(t, baseline("openapi"), func(doc map[string]any) {
		doc["components"] = map[string]any{"schemas": components}
		at(doc, "paths", "/items", "post", "requestBody", "content", "application/json")["schema"] = schema
	})
}
