package style

// Spacing is a typed spacing step matching tokens.SpacingScale entries.
// Values serialize to the Tailwind spacing key (e.g., "3.5", "4",
// "px"), not to a complete utility class. The ClassList builder
// combines a Spacing with its role (padding, margin, width, height,
// gap) to produce the final Tailwind class.
type Spacing string

// Spacing steps — names align with tokens.SpacingScale and the Tailwind
// default spacing scale. Fractional values use underscore for the
// decimal point in the Go const name (S0_5 → "0.5").
const (
	SPX   Spacing = "px"   // 1px
	S0    Spacing = "0"    // 0rem
	S0_5  Spacing = "0.5"  // 0.125rem
	S1    Spacing = "1"    // 0.25rem
	S1_5  Spacing = "1.5"  // 0.375rem
	S2    Spacing = "2"    // 0.5rem
	S2_5  Spacing = "2.5"  // 0.625rem
	S3    Spacing = "3"    // 0.75rem
	S3_5  Spacing = "3.5"  // 0.875rem
	S4    Spacing = "4"    // 1rem
	S5    Spacing = "5"    // 1.25rem
	S6    Spacing = "6"    // 1.5rem
	S7    Spacing = "7"    // 1.75rem
	S8    Spacing = "8"    // 2rem
	S9    Spacing = "9"    // 2.25rem
	S10   Spacing = "10"   // 2.5rem
	S11   Spacing = "11"   // 2.75rem
	S12   Spacing = "12"   // 3rem
	S14   Spacing = "14"   // 3.5rem
	S16   Spacing = "16"   // 4rem
	S20   Spacing = "20"   // 5rem
	S24   Spacing = "24"   // 6rem
	S28   Spacing = "28"   // 7rem
	S32   Spacing = "32"   // 8rem
	S36   Spacing = "36"   // 9rem
	S40   Spacing = "40"   // 10rem
	S44   Spacing = "44"   // 11rem
	S48   Spacing = "48"   // 12rem
	S52   Spacing = "52"   // 13rem
	S56   Spacing = "56"   // 14rem
	S60   Spacing = "60"   // 15rem
	S64   Spacing = "64"   // 16rem
	S72   Spacing = "72"   // 18rem
	S80   Spacing = "80"   // 20rem
	S96   Spacing = "96"   // 24rem
	SAuto Spacing = "auto" // auto
	SFull Spacing = "full" // 100% (width/height only)
)

// AllSpacings returns every Spacing const in stable order.
// Used by the typed tables and All* enumerators to validate compile-table coverage.
func AllSpacings() []Spacing {
	return []Spacing{
		SPX, S0, S0_5, S1, S1_5, S2, S2_5, S3, S3_5, S4,
		S5, S6, S7, S8, S9, S10, S11, S12, S14, S16,
		S20, S24, S28, S32, S36, S40, S44, S48, S52, S56,
		S60, S64, S72, S80, S96,
		SAuto, SFull,
	}
}
