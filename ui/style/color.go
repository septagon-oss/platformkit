package style

// Color is a typed semantic color role. Values serialize to the
// semantic token name (e.g., "surface-primary"), not a Tailwind class
// fragment. The ClassList builder combines a Color with its role
// (background, text, border, ring) to produce the final Tailwind
// class.
type Color string

// IsZero reports whether the Color is the zero value.
func (c Color) IsZero() bool { return c == "" }

// String returns the semantic token name.
func (c Color) String() string { return string(c) }

// Surface colors — backgrounds and filled surfaces.
const (
	SurfacePrimary     Color = "surface-primary"
	SurfaceSecondary   Color = "surface-secondary"
	SurfaceTertiary    Color = "surface-tertiary"
	SurfaceBrand       Color = "surface-brand"
	SurfaceBrandHover  Color = "surface-brand-hover"
	SurfaceBrandSoft   Color = "surface-brand-soft"
	SurfaceSuccess     Color = "surface-success"
	SurfaceSuccessSoft Color = "surface-success-soft"
	SurfaceWarning     Color = "surface-warning"
	SurfaceWarningSoft Color = "surface-warning-soft"
	SurfaceDanger      Color = "surface-danger"
	SurfaceDangerSoft  Color = "surface-danger-soft"
	SurfaceInfo        Color = "surface-info"
	SurfaceInfoSoft    Color = "surface-info-soft"
	SurfaceDisabled    Color = "surface-disabled"
	SurfaceHover       Color = "surface-hover"
	SurfaceActive      Color = "surface-active"
	SurfaceOverlay     Color = "surface-overlay"
	SurfaceInverse     Color = "surface-inverse"
)

// Foreground colors — text and icons.
const (
	FgPrimary     Color = "fg-primary"
	FgSecondary   Color = "fg-secondary"
	FgTertiary    Color = "fg-tertiary"
	FgMuted       Color = "fg-muted"
	FgPlaceholder Color = "fg-placeholder"
	FgBrand       Color = "fg-brand"
	FgOnBrand     Color = "fg-on-brand"
	FgSuccess     Color = "fg-success"
	FgWarning     Color = "fg-warning"
	FgDanger      Color = "fg-danger"
	FgInfo        Color = "fg-info"
	FgDisabled    Color = "fg-disabled"
	FgOnSurface   Color = "fg-on-surface"
	FgOnInverse   Color = "fg-on-inverse"
	FgLink        Color = "fg-link"
	FgLinkHover   Color = "fg-link-hover"
)

// Border colors — divider lines and outlines.
const (
	BorderPrimary   Color = "border-primary"
	BorderSecondary Color = "border-secondary"
	BorderBrand     Color = "border-brand"
	BorderSuccess   Color = "border-success"
	BorderWarning   Color = "border-warning"
	BorderDanger    Color = "border-danger"
	BorderInfo      Color = "border-info"
)

// Ring colors — focus ring and highlights.
const (
	RingBrand  Color = "ring-brand"
	RingFocus  Color = "ring-focus"
	RingDanger Color = "ring-danger"
)

// ColorSpecialTransparent is a sentinel for `transparent` backgrounds.
// Used when a variant needs to explicitly disable a color slot (e.g.,
// ghost/link buttons have transparent backgrounds). It compiles to
// "bg-transparent", "text-transparent", etc. depending on role.
const ColorTransparent Color = "transparent"

// ColorWhite / ColorBlack are Tailwind's plain neutral colors. Prefer
// the semantic Fg/Surface family; use these only for hard-coded
// contrast cases like a button's fill-on-brand text.
const (
	ColorWhite Color = "white"
	ColorBlack Color = "black"
)

// AllColors returns every semantic color const in stable order.
// Used by the typed tables and All* enumerators to validate compile-table coverage.
func AllColors() []Color {
	return []Color{
		// Surface
		SurfacePrimary, SurfaceSecondary, SurfaceTertiary,
		SurfaceBrand, SurfaceBrandHover, SurfaceBrandSoft,
		SurfaceSuccess, SurfaceSuccessSoft,
		SurfaceWarning, SurfaceWarningSoft,
		SurfaceDanger, SurfaceDangerSoft,
		SurfaceInfo, SurfaceInfoSoft,
		SurfaceDisabled, SurfaceHover, SurfaceActive,
		SurfaceOverlay, SurfaceInverse,
		// Foreground
		FgPrimary, FgSecondary, FgTertiary, FgMuted, FgPlaceholder,
		FgBrand, FgOnBrand,
		FgSuccess, FgWarning, FgDanger, FgInfo,
		FgDisabled, FgOnSurface, FgOnInverse,
		FgLink, FgLinkHover,
		// Border
		BorderPrimary, BorderSecondary, BorderBrand,
		BorderSuccess, BorderWarning, BorderDanger, BorderInfo,
		// Ring
		RingBrand, RingFocus, RingDanger,
		// Sentinel
		ColorTransparent,
	}
}
