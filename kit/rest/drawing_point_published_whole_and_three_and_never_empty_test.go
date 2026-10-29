package rest

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// sketchPointNumbers is how many numbers one point of a drawing holds, read from the value's own
// tuple type so a published arity is judged against the value and not against a number typed here.
var sketchPointNumbers = reflect.TypeOf(entity.SketchPoint{}).Len()

// drawingParts names the published tuple's positions in the order the value parses them.
var drawingParts = []string{"the x", "the y", "elapsed milliseconds"}

// decodeDrawing asks the value what it makes of a JSON drawing.
func decodeDrawing(body string) error {
	var s entity.Sketch
	return json.Unmarshal([]byte(body), &s)
}

// TestThePublishedDrawingShapeMatchesWhatTheValueParses reads the shape half of a drawing field back
// off the schema the resource publishes and compares it with what the value parses. The bounds of a
// drawing — how many strokes, points, how far a coordinate may travel — come from entity.SketchMax*,
// and the sentence beside them is pinned elsewhere; what is pinned here is the part no figure states:
// a point is exactly the tuple the value reads, every number it holds is whole, none of them is
// negative, a stroke carries at least one point, and a field the body may leave unset stays sendable
// both empty and absent. An API client generates its types from this schema, so each of these
// published as something looser than the value tells the caller — the phone among them — to send a
// body its own server then refuses, and a client learns that only after the round trip.
func TestThePublishedDrawingShapeMatchesWhatTheValueParses(t *testing.T) {
	// The smallest drawing the value accepts, so a refusal below means the published shape, not
	// the value, has moved.
	if err := decodeDrawing(`[[[0,0,0]]]`); err != nil {
		t.Fatalf("the value refuses [[[0,0,0]]], the smallest drawing the schema can describe: %v", err)
	}

	optional := publishedDrawingField(t, entity.Field{Name: "drawing", Type: entity.TypeSketch})
	stroke := optional.Items
	point := stroke.Items
	prefix, _ := point.Extensions["prefixItems"].([]*huma.Schema)

	if len(prefix) != sketchPointNumbers {
		t.Errorf("the published point tuple names %d parts, want the %d numbers the value parses per point", len(prefix), sketchPointNumbers)
	}
	if point.MinItems == nil || *point.MinItems != sketchPointNumbers || point.MaxItems == nil || *point.MaxItems != sketchPointNumbers {
		t.Errorf("the published point array allows %s to %s numbers, want exactly the %d the value parses per point",
			arityLabel(point.MinItems), arityLabel(point.MaxItems), sketchPointNumbers)
	}
	if err := decodeDrawing(`[[[1,2]]]`); err == nil {
		t.Error("the value accepted a two-number point, so the published arity is no longer the value's")
	}

	if point.Items == nil || point.Items.Type != huma.TypeInteger {
		t.Errorf("the published point array holds %s, want integers: %s", typeLabel(point.Items), wholeNumbersRefused)
	}
	for i, part := range prefix {
		if i >= len(drawingParts) {
			break
		}
		if part == nil || part.Type != huma.TypeInteger {
			t.Errorf("the published schema holds %s as %s, want integers: %s", drawingParts[i], typeLabel(part), wholeNumbersRefused)
		}
		if part.Minimum == nil || *part.Minimum != 0 {
			t.Errorf("the published schema lets %s go below %s, want the floor 0 the value refuses below", drawingParts[i], boundLabel(part.Minimum))
		}
	}
	if err := decodeDrawing(`[[[1.5,0,0]]]`); err == nil {
		t.Error("the value accepted a fractional coordinate, so the published integer type is no longer the value's")
	}
	if err := decodeDrawing(`[[[-1,0,0]]]`); err == nil {
		t.Error("the value accepted a negative coordinate, so the published floor of 0 is no longer the value's")
	}
	if err := decodeDrawing(`[[[0,0,-1]]]`); err == nil {
		t.Error("the value accepted a point before the start of its stroke, so the published floor of 0 is no longer the value's")
	}

	if stroke.MinItems == nil || *stroke.MinItems < 1 {
		t.Errorf("the published stroke allows %s points, want at least one: the value refuses a stroke with none", arityLabel(stroke.MinItems))
	}
	if err := decodeDrawing(`[[0,0,0],[]]`); err == nil {
		t.Error("the value accepted a stroke with no points, so the published stroke minimum is no longer the value's")
	}

	if optional.Nullable != true {
		t.Error("a drawing the body may leave unset is published as never null, so a client is told it cannot leave the field out")
	}
	if optional.MinItems != nil {
		t.Errorf("a drawing the body may leave unset is published with a minimum of %s strokes, so a client is told it cannot clear the drawing by sending it empty", arityLabel(optional.MinItems))
	}
	if err := decodeDrawing(`[]`); err != nil {
		t.Errorf("the value refuses an explicitly empty drawing, which the schema publishes as sendable: %v", err)
	}

	required := publishedDrawingField(t, entity.Field{Name: "drawing", Type: entity.TypeSketch, Required: true})
	if required.Nullable != false {
		t.Error("a drawing the body must carry is published as nullable")
	}
	if required.MinItems == nil || *required.MinItems != 1 {
		t.Errorf("a drawing the body must carry is published with a minimum of %s strokes, want one", arityLabel(required.MinItems))
	}
}

// publishedDrawingField publishes one field through the kernel's own annotation and returns the
// schema it wrote for it.
func publishedDrawingField(t *testing.T, field entity.Field) *huma.Schema {
	t.Helper()
	schema := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}}
	annotateSketchFields(schema, []entity.Field{field})
	property := schema.Properties[field.Name]
	if property == nil || property.Items == nil || property.Items.Items == nil {
		t.Fatalf("%s published no stroke schema: %#v", field.Name, property)
	}
	return property
}

// wholeNumbersRefused names, once, the refusal every integer assertion rests on.
const wholeNumbersRefused = "the value refuses a point that is not a whole number"

// arityLabel renders an optional item count for a failure message.
func arityLabel(count *int) string {
	if count == nil {
		return "no bound"
	}
	return strconv.Itoa(*count)
}

// boundLabel renders an optional numeric bound for a failure message.
func boundLabel(bound *float64) string {
	if bound == nil {
		return "no bound"
	}
	return strconv.FormatFloat(*bound, 'f', -1, 64)
}

// typeLabel renders a published schema's type for a failure message.
func typeLabel(schema *huma.Schema) string {
	if schema == nil {
		return "nothing"
	}
	return string(schema.Type)
}
