package rest

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// annotateSketchFields publishes drawing bounds on an existing object schema.
// Tuple semantics and aggregate counts are additionally checked by the value.
func annotateSketchFields(schema *huma.Schema, fields []entity.Field) {
	for _, f := range fields {
		if f.Type != entity.TypeSketch {
			continue
		}
		coordinate := &huma.Schema{Type: huma.TypeInteger, Minimum: new(float64(0)), Maximum: new(float64(entity.SketchMaxCoordinate))}
		elapsed := &huma.Schema{Type: huma.TypeInteger, Minimum: new(float64(0)), Maximum: new(float64(entity.SketchMaxMilliseconds))}
		point := &huma.Schema{Type: huma.TypeArray, MinItems: new(3), MaxItems: new(3), Items: &huma.Schema{Type: huma.TypeInteger}, Extensions: map[string]any{"prefixItems": []*huma.Schema{coordinate, coordinate, elapsed}}}
		stroke := &huma.Schema{Type: huma.TypeArray, MinItems: new(1), MaxItems: new(entity.SketchMaxPoints), Items: point}
		property := &huma.Schema{Type: huma.TypeArray, Nullable: !f.Required, ReadOnly: f.ReadOnly, MaxItems: new(entity.SketchMaxStrokes), Items: stroke,
			Description: "Strokes of integer [x,y,ms] tuples. At most 20,000 points in total; each stroke starts at 0 milliseconds with nondecreasing elapsed times."}
		if f.Required {
			property.MinItems = new(1)
		}
		schema.Properties[f.Name] = property
	}
}
