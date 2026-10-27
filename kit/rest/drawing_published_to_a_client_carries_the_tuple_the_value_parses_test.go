package rest

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// publishedDrawingBytes publishes one drawing field through the kernel's annotation and returns the
// JSON its schema marshals to — the bytes a client generator reads, one step further out than the
// schema struct the shape cases read. Required asks for a field the body must carry.
func publishedDrawingBytes(t *testing.T, required bool) map[string]any {
	t.Helper()
	field := entity.Field{Name: "drawing", Type: entity.TypeSketch}
	if required {
		field.Required = true
	}
	schema := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}}
	annotateSketchFields(schema, []entity.Field{field})
	document, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("the drawing schema does not marshal: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(document, &object); err != nil {
		t.Fatalf("the published drawing schema is not JSON: %v", err)
	}
	properties, _ := object["properties"].(map[string]any)
	drawing, _ := properties["drawing"].(map[string]any)
	if drawing == nil {
		t.Fatalf("the published bytes hold no drawing field: %s", document)
	}
	return drawing
}

// publishedCount reads one published item count, and reports whether the bytes state one at all.
func publishedCount(published map[string]any, keyword string) (int, bool) {
	value, held := published[keyword]
	if !held {
		return 0, false
	}
	number, ok := value.(float64)
	return int(number), ok
}

// publishedPart returns one named part of the published tuple, and whether the bytes name it.
func publishedPart(t *testing.T, tuple []any, part int) map[string]any {
	t.Helper()
	if part >= len(tuple) {
		t.Fatalf("the published tuple holds %d parts, want a part %d in it", len(tuple), part)
	}
	named, _ := tuple[part].(map[string]any)
	if named == nil {
		t.Fatalf("published tuple part %d is not an object: %#v", part, tuple[part])
	}
	return named
}

// TestThePublishedDrawingReachesAClientAsTheTupleTheValueParses follows a drawing field out of the
// schema struct into the bytes an API client generates its types from. The bounds and the shape are
// read off the struct elsewhere; what is read here is what survives the marshalling, because a bound
// the server holds but never publishes leaves a client to guess it, and one published only inside a
// keyword the marshaller drops is no contract at all. So each claim is asked of the JSON: the tuple
// the value parses, published as its own keyword and not only as an array of numbers; every number
// of it whole, floored where the value floors and capped at the value's ceilings; one point per
// stroke at least; and a field the body may leave unset still sendable absent, empty and null while
// a field the body must carry is neither null nor empty.
func TestThePublishedDrawingReachesAClientAsTheTupleTheValueParses(t *testing.T) {
	optional := publishedDrawingBytes(t, false)
	if types, _ := optional["type"].([]any); len(types) != 2 || types[0] != "array" || types[1] != "null" {
		t.Errorf("a drawing the body may leave unset publishes type %v, want an array that may be null", optional["type"])
	}
	if count, held := publishedCount(optional, "minItems"); held {
		t.Errorf("a drawing the body may leave unset publishes a minimum of %d strokes, so a client is told it cannot clear the drawing by sending it empty", count)
	}

	required := publishedDrawingBytes(t, true)
	if required["type"] != "array" {
		t.Errorf("a drawing the body must carry publishes type %v, want an array that may not be null", required["type"])
	}
	if count, held := publishedCount(required, "minItems"); !held || count != 1 {
		t.Errorf("a drawing the body must carry publishes a minimum of %d strokes, want one", count)
	}

	stroke, _ := optional["items"].(map[string]any)
	if stroke == nil || stroke["type"] != "array" {
		t.Fatalf("a drawing publishes no stroke array: %#v", optional["items"])
	}
	if count, held := publishedCount(stroke, "minItems"); !held || count < 1 {
		t.Errorf("a stroke publishes a minimum of %d points, want at least one, which is the least the value parses", count)
	}
	if count, held := publishedCount(stroke, "maxItems"); !held || int64(count) != entity.SketchMaxPoints {
		t.Errorf("a stroke publishes a maximum of %d points, want the value's %d", count, entity.SketchMaxPoints)
	}

	point, _ := stroke["items"].(map[string]any)
	if point == nil || point["type"] != "array" {
		t.Fatalf("a stroke publishes no point array: %#v", stroke["items"])
	}
	low, hasLow := publishedCount(point, "minItems")
	high, hasHigh := publishedCount(point, "maxItems")
	if !hasLow || !hasHigh || low != high || low != sketchTupleNumbers {
		t.Errorf("a point publishes an arity of %d to %d numbers, want exactly the %d the value parses", low, high, sketchTupleNumbers)
	}
	element, _ := point["items"].(map[string]any)
	if element == nil || element["type"] != "integer" {
		t.Errorf("a point publishes its numbers as %v, want integers the way the value refuses every other number", element["type"])
	}

	parts, _ := point["prefixItems"].([]any)
	if len(parts) != sketchTupleNumbers {
		t.Fatalf("the published bytes name %d parts of a point, want the %d the value parses, each with its own bounds", len(parts), sketchTupleNumbers)
	}
	for part, ceiling := range sketchTupleCeilings {
		named := publishedPart(t, parts, part)
		if named["type"] != "integer" {
			t.Errorf("the published point's %s is %v, want integers the way the value refuses every other number", sketchTupleParts[part], named["type"])
		}
		if bound, held := named["minimum"].(float64); !held || bound != 0 {
			t.Errorf("the published point's %s carries no floor of 0, which is where the value refuses to go", sketchTupleParts[part])
		}
		if bound, held := named["maximum"].(float64); !held || int64(bound) != ceiling {
			t.Errorf("the published point's %s carries a ceiling of %v, want the value's %d", sketchTupleParts[part], named["maximum"], ceiling)
		}
	}
}

// sketchTupleNumbers is the arity of one point, read from the value's own tuple type.
const sketchTupleNumbers = 3

// sketchTupleParts names the published tuple's positions in the order the value parses them.
var sketchTupleParts = []string{"x", "y", "elapsed milliseconds"}

// sketchTupleCeilings are the ceilings the value enforces on each part of a point, in the same order.
var sketchTupleCeilings = []int64{
	entity.SketchMaxCoordinate, entity.SketchMaxCoordinate, entity.SketchMaxMilliseconds,
}
