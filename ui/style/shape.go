package style

// Shape is a typed component shape. Values match the canonical keys
// in base.Component shape semantics.
type Shape string

// IsZero reports whether the Shape is the zero value.
func (s Shape) IsZero() bool { return s == "" }

// String returns the canonical shape key.
func (s Shape) String() string { return string(s) }

const (
	ShapeSquare  Shape = "square"
	ShapeRounded Shape = "rounded"
	ShapeCircle  Shape = "circle"
	ShapePill    Shape = "pill"
)

// AllShapes returns every Shape const in stable order.
func AllShapes() []Shape {
	return []Shape{
		ShapeSquare, ShapeRounded, ShapeCircle, ShapePill,
	}
}
