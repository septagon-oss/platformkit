package style

// Shadow is a typed box-shadow step matching tokens.ShadowScale.
type Shadow string

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
