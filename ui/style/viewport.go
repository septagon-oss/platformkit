package style

// ViewportHeight is the closed set of viewport-relative heights used by
// governed overlays. Keeping these values typed lets CSS emission remain
// fail-closed while supporting responsive drawers and sheets.
type ViewportHeight string

const (
	VH25  ViewportHeight = "25"
	VH50  ViewportHeight = "50"
	VH75  ViewportHeight = "75"
	VH85  ViewportHeight = "85"
	VH100 ViewportHeight = "100"
)

// AllViewportHeights returns every supported viewport height in stable order.
func AllViewportHeights() []ViewportHeight {
	return []ViewportHeight{VH25, VH50, VH75, VH85, VH100}
}
