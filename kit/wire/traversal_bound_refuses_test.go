package wire_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// The two limits of one analysis: the depth of a single schema path, and the number of
// schema nodes one document may visit. Both are stated here as the literals the package
// README quotes, so widening either one is a change a reviewer sees twice.
const (
	depthLimit  = 24
	nodeBudget  = 200_000
	sharedProps = 500
	sharedUses  = 450
)

// nestedSchemas is a schema with the leaf `depth` levels below its own node, each level one
// object property named child.
func nestedSchemas(depth int, leaf map[string]any) map[string]any {
	if depth == 0 {
		return leaf
	}
	return map[string]any{"type": "object", "properties": map[string]any{"child": nestedSchemas(depth-1, leaf)}}
}

// nestedAddress is the shape address the leaf of nestedSchemas(depth) is recorded at.
func nestedAddress(depth int) string {
	return shapeKey + strings.Repeat(" .child", depth)
}

// withRequestSchema returns baseline("openapi") with its request body schema replaced.
func withRequestSchema(t *testing.T, schema map[string]any) []byte {
	t.Helper()
	return mutated(t, baseline("openapi"), func(doc map[string]any) {
		at(doc, "paths", "/items", "post", "requestBody", "content", "application/json")["schema"] = schema
	})
}

func leafOfType(kind string) map[string]any {
	return map[string]any{"type": kind}
}

// A shape at the limit is read, and a shape past it is refused as unfinished rather than
// answered as compatible: the comparison says so on both sides of the bound.
func TestTheDepthLimitIsReadOnBothSides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		depth int
	}{
		{"a leaf at the limit is compared", depthLimit},
		{"a leaf one level past the limit refuses", depthLimit + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := withRequestSchema(t, nestedSchemas(tc.depth, leafOfType("string")))
			fresh := withRequestSchema(t, nestedSchemas(tc.depth, leafOfType("integer")))
			breaks := wire.Compare(old, fresh)
			if tc.depth == depthLimit {
				want := []string{closedShape(nestedAddress(tc.depth), "type=string", "type=integer")}
				if got := refusalsAsMessages(t, breaks, "B3"); !slices.Equal(got, want) {
					t.Fatalf("got %v; want %v", breaks, want)
				}
				return
			}
			if len(breaks) == 0 {
				t.Fatal("a leaf below the limit read as no change at all")
			}
			got := refusalsAsMessages(t, breaks, "B7")
			if len(got) != 2 || !strings.Contains(got[0], nestedAddress(tc.depth)) {
				t.Fatalf("got %v; want both sides refused as unfinished at %s", breaks, nestedAddress(tc.depth))
			}
			for _, message := range got {
				if !strings.Contains(message, "past the traversal limit of 24 schema levels") {
					t.Fatalf("the refusal does not name the limit that stopped it: %v", breaks)
				}
			}
		})
	}
}

// Two identical documents that cannot be read in full are not a certified pair: the refusal
// arrives on equal bytes, so an unfinished analysis cannot be the reason a baseline moves.
func TestAnUnreadableDocumentRefusesEvenWhenNothingChanged(t *testing.T) {
	t.Parallel()
	deep := withRequestSchema(t, nestedSchemas(depthLimit+1, leafOfType("string")))
	breaks := wire.Compare(deep, deep)
	if len(refusalsAsMessages(t, breaks, "B7")) != 2 {
		t.Fatalf("got %v; want one refusal per side", breaks)
	}
	for _, b := range breaks {
		if b.Member != nestedAddress(depthLimit+1) {
			t.Fatalf("got %v; want the refusal to name %s", breaks, nestedAddress(depthLimit+1))
		}
	}
	shallow := withRequestSchema(t, nestedSchemas(depthLimit, leafOfType("string")))
	if breaks := wire.Compare(shallow, shallow); len(breaks) != 0 {
		t.Fatalf("a document read in full must certify, got %v", breaks)
	}
}

// A reference chain is as deep as a nesting: the limit bounds the walk, not the syntax it
// walks, and a tail below it is refused rather than answered as nothing.
func TestAReferenceChainPastTheLimitRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		golden, current string
	}{
		{"an unchanged chain is unfinished, not certified", "string", "string"},
		{"a tail below the limit is never compared", "string", "integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := withChain(t, chainSchemas(tc.golden))
			fresh := withChain(t, chainSchemas(tc.current))
			breaks := wire.Compare(old, fresh)
			if got := refusalsAsMessages(t, breaks, "B7"); len(got) != 2 {
				t.Fatalf("got %v; want the chain refused as unfinished on both sides", breaks)
			}
		})
	}
}

// chainSchemas is a chain of references deeper than the traversal limit, ending in a leaf of
// the given kind.
func chainSchemas(leaf string) map[string]any {
	schemas := map[string]any{}
	for i := range depthLimit + 2 {
		schemas["Chain"+strconv.Itoa(i)] = map[string]any{"type": "object", "properties": map[string]any{
			"next": map[string]any{"$ref": "#/components/schemas/Chain" + strconv.Itoa(i+1)},
		}}
	}
	schemas["Chain"+strconv.Itoa(depthLimit+2)] = map[string]any{"type": "object", "properties": map[string]any{
		"leaf": leafOfType(leaf),
	}}
	return schemas
}

// withChain is a contract whose request body schema is the head of a chain of references.
func withChain(t *testing.T, schemas map[string]any) []byte {
	t.Helper()
	return mutated(t, baseline("openapi"), func(doc map[string]any) {
		doc["components"] = map[string]any{"schemas": schemas}
		at(doc, "paths", "/items", "post", "requestBody", "content", "application/json")["schema"] = map[string]any{
			"$ref": "#/components/schemas/Chain0",
		}
	})
}

// The node budget bounds the work several paths do to one shared component, which no depth
// bound bounds: a component reached by many operations is expanded on every path that
// reaches it.
func TestTheNodeBudgetRefusesRatherThanRunningUnbounded(t *testing.T) {
	t.Parallel()
	wide := sharedSchemaDocument()
	breaks := wire.Compare(wide, wide)
	if len(breaks) == 0 {
		t.Fatal("a document the walk gave up on read as compatible")
	}
	messages := refusalsAsMessages(t, breaks, "B7")
	for _, message := range messages {
		if !strings.Contains(message, "after 200000 schema nodes") {
			t.Fatalf("the refusal does not name the budget that stopped it: %s", message)
		}
	}
	if again := wire.Compare(wide, wide); !reflect.DeepEqual(breaks, again) {
		t.Fatal("two runs of one comparison disagreed, so the named address is not a fact")
	}
}

// sharedSchemaDocument is a contract of sharedUses operations whose bodies are one component
// schema of sharedProps properties: the work is the product of the paths, not of the depth.
func sharedSchemaDocument() []byte {
	properties := map[string]any{}
	for i := range sharedProps {
		properties["field"+strconv.Itoa(i)] = leafOfType("string")
	}
	paths := map[string]any{}
	for i := range sharedUses {
		id := "op" + strconv.Itoa(i)
		paths["/r"+id] = map[string]any{"post": map[string]any{
			"operationId":        id,
			"x-platformkit-auth": map[string]any{"kind": "public"},
			"requestBody": map[string]any{"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Row"}},
			}},
			"responses": map[string]any{},
		}}
	}
	body, err := json.Marshal(map[string]any{
		"openapi": "3.1.0",
		"paths":   paths,
		"components": map[string]any{"schemas": map[string]any{
			"Row": map[string]any{"type": "object", "properties": properties},
		}},
	})
	if err != nil {
		panic(err)
	}
	return body
}
