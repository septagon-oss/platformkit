package style

// Shadow is a typed box-shadow step matching tokens.ShadowScale.
type Shadow string

// IsZero reports whether the Shadow is the zero value.
func (s Shadow) IsZero() bool { return s == "" }

// String returns the shadow key.
func (s Shadow) String() string { return string(s) }

const (
	ShadowNone  Shadow = "none"
	ShadowSM    Shadow = "sm"
	ShadowBase  Shadow = "base"
	ShadowMD    Shadow = "md"
	ShadowLG    Shadow = "lg"
	ShadowXL    Shadow = "xl"
	Shadow2XL   Shadow = "2xl"
	ShadowInner Shadow = "inner"
)

// AllShadows returns every Shadow const in stable order.
func AllShadows() []Shadow {
	return []Shadow{
		ShadowNone, ShadowSM, ShadowBase, ShadowMD,
		ShadowLG, ShadowXL, Shadow2XL, ShadowInner,
	}
}
