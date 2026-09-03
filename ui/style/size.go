package style

// MaxWidth is a typed handle for Tailwind's named max-width scale.
// Values serialize to the Tailwind key ("sm", "2xl", "full") and
// flow through ClassList.MaxWScaled to produce the "max-w-<key>" utility.
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
