package style

import "fmt"

// ZLayer is a typed semantic z-index layer. The underlying value is
// the numeric z-index used across the design system (see tokens.ZIndexScale).
// The Class() method converts a ZLayer to its Tailwind arbitrary-value
// class (e.g., ZModal → "z-[1400]"), so there is no "magic z-50 vs z-70"
// — every layer's number comes from the typed tokens.
type ZLayer int16

// IsZero reports whether the ZLayer is the zero value (ZBase).
// ZBase semantically means "default flow layer", but treat as zero.
func (z ZLayer) IsZero() bool { return z == 0 }

// Class returns the Tailwind arbitrary-value z-index class.
// For negative layers, emits "-z-[N]"; for positive, "z-[N]".
func (z ZLayer) Class() string {
	if z < 0 {
		return fmt.Sprintf("-z-[%d]", -int(z))
	}
	return fmt.Sprintf("z-[%d]", int(z))
}

// String returns the numeric value as a decimal string.
func (z ZLayer) String() string {
	return fmt.Sprintf("%d", int(z))
}

// Semantic z-index layers. Numeric values match tokens.DefaultZIndex
// and are the single source of truth. Changing the value here is the
// only place a z-index number should ever appear.
const (
	ZBelow    ZLayer = -10
	ZBase     ZLayer = 0
	ZDocked   ZLayer = 10
	ZDropdown ZLayer = 1000
	ZSticky   ZLayer = 1100
	ZBanner   ZLayer = 1200
	ZOverlay  ZLayer = 1300
	ZModal    ZLayer = 1400
	ZPopover  ZLayer = 1500
	ZToast    ZLayer = 1600
	ZTooltip  ZLayer = 1700
)

// AllZLayers returns every ZLayer const in stable order.
func AllZLayers() []ZLayer {
	return []ZLayer{
		ZBelow, ZBase, ZDocked, ZDropdown, ZSticky,
		ZBanner, ZOverlay, ZModal, ZPopover, ZToast, ZTooltip,
	}
}
