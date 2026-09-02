package style

// Variant is a typed component visual variant. Component builders
// accept this type — not raw strings — as the variant input.
//
// Values serialize to the canonical variant name (e.g., "primary")
// used across the design manifest, Storybook stories, and CSS class
// resolution maps. The design system's VariantMap type is keyed on
// string for Go-ergonomic lookup; callers should use variant.String()
// when interacting with those maps.
type Variant string

// IsZero reports whether the Variant is the zero value.
func (v Variant) IsZero() bool { return v == "" }

// String returns the canonical variant name.
func (v Variant) String() string { return string(v) }

// Canonical variant names. Match the keys used in ButtonTokens.Variants,
// BadgeTokens.Variants, and other VariantMap consumers.
const (
	VariantDefault   Variant = "default"
	VariantPrimary   Variant = "primary"
	VariantSecondary Variant = "secondary"
	VariantSuccess   Variant = "success"
	VariantWarning   Variant = "warning"
	VariantError     Variant = "error"
	VariantDanger    Variant = "danger" // alias of VariantError for domain code that prefers "danger"
	VariantInfo      Variant = "info"
	VariantOutline   Variant = "outline"
	VariantGhost     Variant = "ghost"
	VariantLink      Variant = "link"
)

// AllVariants returns every canonical Variant in stable order.
func AllVariants() []Variant {
	return []Variant{
		VariantDefault, VariantPrimary, VariantSecondary,
		VariantSuccess, VariantWarning, VariantError, VariantDanger,
		VariantInfo, VariantOutline, VariantGhost, VariantLink,
	}
}
