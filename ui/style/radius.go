package style

// Radius is a typed border-radius step matching tokens.RadiusScale.
type Radius string

// IsZero reports whether the Radius is the zero value.
func (r Radius) IsZero() bool { return r == "" }

// String returns the radius key.
func (r Radius) String() string { return string(r) }

const (
	RadiusNone Radius = "none"
	RadiusSM   Radius = "sm"
	RadiusBase Radius = "base"
	RadiusMD   Radius = "md"
	RadiusLG   Radius = "lg"
	RadiusXL   Radius = "xl"
	Radius2XL  Radius = "2xl"
	Radius3XL  Radius = "3xl"
	RadiusFull Radius = "full"
)

// AllRadii returns every Radius const in stable order.
func AllRadii() []Radius {
	return []Radius{
		RadiusNone, RadiusSM, RadiusBase, RadiusMD,
		RadiusLG, RadiusXL, Radius2XL, Radius3XL, RadiusFull,
	}
}
