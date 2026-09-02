package style

// Size is a typed component size step. Values match the canonical keys
// in ButtonTokens.Sizes, InputTokens.Sizes, and other SizeMap consumers.
type Size string

// IsZero reports whether the Size is the zero value.
func (s Size) IsZero() bool { return s == "" }

// String returns the canonical size key.
func (s Size) String() string { return string(s) }

const (
	SizeXS     Size = "xs"
	SizeSmall  Size = "sm"
	SizeMedium Size = "md"
	SizeLarge  Size = "lg"
	SizeXL     Size = "xl"
	Size2XL    Size = "2xl"
)

// AllSizes returns every Size const in stable order.
func AllSizes() []Size {
	return []Size{
		SizeXS, SizeSmall, SizeMedium, SizeLarge, SizeXL, Size2XL,
	}
}

// MaxWidth is a typed handle for Tailwind's named max-width scale.
// Values serialize to the Tailwind key ("sm", "2xl", "full") and
// flow through classes.MaxW to produce the "max-w-<key>" utility.
type MaxWidth string

const (
	MaxWXS     MaxWidth = "xs"
	MaxWSM     MaxWidth = "sm"
	MaxWMD     MaxWidth = "md"
	MaxWLG     MaxWidth = "lg"
	MaxWXL     MaxWidth = "xl"
	MaxW2XL    MaxWidth = "2xl"
	MaxW3XL    MaxWidth = "3xl"
	MaxW4XL    MaxWidth = "4xl"
	MaxW5XL    MaxWidth = "5xl"
	MaxW6XL    MaxWidth = "6xl"
	MaxW7XL    MaxWidth = "7xl"
	MaxWFull   MaxWidth = "full"
	MaxWNone   MaxWidth = "none"
	MaxWScreen MaxWidth = "screen"
	MaxWProse  MaxWidth = "prose"
)
