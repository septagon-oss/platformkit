package components

// GridProps defines properties for a CSS Grid layout.
type GridProps struct {
	ComponentProps

	Columns string `json:"columns,omitempty" enum:",1,2,3,4,6,12" doc:"Base column count; empty uses one column."`
	SM      string `json:"sm,omitempty" enum:",1,2,3,4,6,12" doc:"Column count from the small breakpoint."`
	MD      string `json:"md,omitempty" enum:",1,2,3,4,6,12" doc:"Column count from the medium breakpoint."`
	LG      string `json:"lg,omitempty" enum:",1,2,3,4,6,12" doc:"Column count from the large breakpoint."`
	Gap     string `json:"gap,omitempty"`
}

// StackProps defines properties for a vertical stack layout.
type StackProps struct {
	ComponentProps

	Gap   string `json:"gap,omitempty"`
	Align string `json:"align,omitempty"` // start, center, end, stretch
}

// FlexProps defines properties for a flexbox layout.
type FlexProps struct {
	ComponentProps

	Direction string `json:"direction,omitempty"` // row, column
	Wrap      bool   `json:"wrap,omitempty"`
	Gap       string `json:"gap,omitempty"`
	Align     string `json:"align,omitempty"`   // start, center, end, stretch
	Justify   string `json:"justify,omitempty"` // start, center, end, between, around
}

// ContainerProps defines properties for a centered container.
type ContainerProps struct {
	ComponentProps

	MaxWidth string `json:"maxWidth,omitempty"` // sm, md, lg, xl, 2xl, full
	Padding  string `json:"padding,omitempty"`
}

// CardProps defines platform-agnostic properties for a Card component.
type CardProps struct {
	ComponentProps
	HTMXProps

	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	Image         string `json:"image,omitempty"`
	ImageAlt      string `json:"imageAlt,omitempty"`
	ImagePosition string `json:"imagePosition,omitempty"` // top, bottom, left, right
	Variant       string `json:"variant,omitempty"`       // default, elevated, outlined, plain
	Padding       string `json:"padding,omitempty"`       // none, small, medium, large
	Shadow        string `json:"shadow,omitempty"`        // none, small, medium, large
	Clickable     bool   `json:"clickable,omitempty"`     // pointer and focus styling; a card with an Href is a link and clickable whatever this says
	Hoverable     bool   `json:"hoverable,omitempty"`
	Href          string `json:"href,omitempty"`
}

type DetailSheetProps struct {
	DetailPanelProps
	Open           bool   `json:"open,omitzero"`
	Placement      string `json:"placement,omitempty"`
	CloseOnOverlay *bool  `json:"closeOnOverlay,omitempty"`
	CloseOnEscape  *bool  `json:"closeOnEscape,omitempty"`
}

// ModalProps retains its centered behavior unless Placement names a sheet.
type ModalProps struct {
	ComponentProps

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Footer      string `json:"footer,omitempty"`
	AriaLabel   string `json:"ariaLabel,omitempty"`
	CloseLabel  string `json:"closeLabel,omitempty"`
	Size        string `json:"size,omitempty"`      // small, medium, large, xl, full
	Placement   string `json:"placement,omitempty"` // bottom, end, auto; empty preserves Centered

	// Pointer booleans preserve the intended default-true behavior while still
	// allowing portable clients to explicitly disable an affordance.
	Closable       *bool `json:"closable,omitempty"`
	CloseOnOverlay *bool `json:"closeOnOverlay,omitempty"`
	CloseOnEscape  *bool `json:"closeOnEscape,omitempty"`
	ShowClose      *bool `json:"showClose,omitempty"`
	ShowOverlay    *bool `json:"showOverlay,omitempty"`
	Centered       *bool `json:"centered,omitempty"`
	ClearOnClose   *bool `json:"clearOnClose,omitempty"`
	Open           bool  `json:"open,omitempty"`
	OpenOnSwap     bool  `json:"openOnSwap,omitempty"`
	Deferred       bool  `json:"deferred,omitempty"`
}
