package components

// BadgeProps defines the platform-agnostic properties for a Badge component.
type BadgeProps struct {
	ComponentProps

	Label       string `json:"label"`
	Variant     string `json:"variant,omitempty"`                                                // primary, secondary, outline
	Tone        string `json:"tone,omitempty" enum:",neutral,brand,success,warning,danger,info"` // neutral, brand, success, warning, danger, info
	Size        string `json:"size,omitempty" enum:",xs,sm,md,lg,xl,2xl"`                        // xs, sm, md, lg, xl, 2xl
	Dot         bool   `json:"dot,omitempty"`                                                    // show status dot before the label
	Count       int    `json:"count,omitempty"`                                                  // positive count, visually capped to 99+
	Removable   bool   `json:"removable,omitempty"`
	RemoveLabel string `json:"removeLabel,omitempty"` // localized remove-button label
	Live        bool   `json:"live,omitempty"`        // polite status announcement
}

// AlertProps defines properties for a persistent inline status message.
type AlertProps struct {
	ComponentProps

	Message     string `json:"message"`
	Title       string `json:"title,omitempty"`
	Tone        string `json:"tone,omitempty"` // neutral, info, success, warning, danger (default info)
	Dismissible bool   `json:"dismissible,omitempty"`
	// DismissLabel names the dismiss control. Empty keeps "Dismiss notification".
	DismissLabel string `json:"dismissLabel,omitempty"`
	// Live overrides the legacy tone-based announcement. off is a non-live note.
	Live     string `json:"live,omitempty" enum:",polite,assertive,off"`
	Bordered bool   `json:"bordered,omitempty"`
	Compact  bool   `json:"compact,omitempty"`
}

// TextProps defines the platform-agnostic properties for a Text component.
type TextProps struct {
	ComponentProps

	Content   string `json:"content"`
	Element   string `json:"element,omitempty"`   // p, span, div, strong, em, small, mark, del, ins, sub, sup, blockquote, code, pre, kbd, samp, var
	Size      string `json:"size,omitempty"`      // xs, sm, base, lg, xl, 2xl, 3xl, 4xl, 5xl
	Align     string `json:"align,omitempty"`     // left, center, right, justify
	Weight    string `json:"weight,omitempty"`    // thin, extralight, light, normal, medium, semibold, bold, extrabold, black
	Color     string `json:"color,omitempty"`     // primary, secondary, tertiary, muted, brand, success, warning, danger, info
	Transform string `json:"transform,omitempty"` // none, uppercase, lowercase, capitalize
	Truncate  bool   `json:"truncate,omitempty"`  // truncate with ellipsis
	NoWrap    bool   `json:"nowrap,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Lines     int    `json:"lines,omitempty"` // line clamp, 1-6
}

// HeadingProps defines properties for heading elements (H1-H6).
type HeadingProps struct {
	ComponentProps

	Text     string `json:"text"`
	Level    int    `json:"level" enum:"0,1,2,3,4,5,6" doc:"Semantic heading level; zero uses h2."`
	Size     int    `json:"size,omitzero" enum:"0,1,2,3,4,5,6" doc:"Visual heading scale, independent of level; zero follows level."`
	Anchor   string `json:"anchor,omitempty"` // optional anchor ID
	Truncate bool   `json:"truncate,omitempty"`
}

// IconProps defines a provider-neutral system glyph.
type IconProps struct {
	ComponentProps

	Name      string `json:"name"`
	Size      string `json:"size,omitempty"`   // xs, sm, md, lg, xl, 2xl
	Tone      string `json:"tone,omitempty"`   // neutral, brand, success, warning, danger, info
	Weight    string `json:"weight,omitempty"` // outline; extension providers may add governed weights
	AriaLabel string `json:"ariaLabel,omitempty"`
}

// DividerProps defines properties for a divider/separator.
type DividerProps struct {
	ComponentProps

	Orientation string `json:"orientation,omitempty"` // horizontal, vertical
	Text        string `json:"text,omitempty"`        // optional label (e.g., "OR")
}

// LinkProps defines properties for a hyperlink.
type LinkProps struct {
	ComponentProps
	HTMXProps

	Label    string `json:"label"`
	Href     string `json:"href"`
	External bool   `json:"external,omitempty"` // opens in new tab
	Variant  string `json:"variant,omitempty"`  // primary, secondary, text, underline
	Target   string `json:"target,omitempty"`
	Rel      string `json:"rel,omitempty"`
}

// SpinnerProps defines properties for a loading spinner.
type SpinnerProps struct {
	ComponentProps

	Label string `json:"label,omitempty"` // sr-only text
	Size  string `json:"size,omitempty"`  // xs, sm, md, lg, xl, 2xl
	Tone  string `json:"tone,omitempty"`  // neutral, brand, success, warning, danger, info (default brand)
}

// EmptyStateProps defines properties for an empty data state placeholder.
type EmptyStateProps struct {
	ComponentProps

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Text takes precedence over Description; Action and the Actions slot are exclusive.
	Text     string       `json:"text,omitempty"`
	Action   *ButtonProps `json:"action,omitempty"`
	Compact  bool         `json:"compact,omitempty"`
	Bordered bool         `json:"bordered,omitempty"`
}

// NoticeProps supplies localized copy and an optional native recovery action.
// Dismissal is local to the rendered notice; it never changes a saved result.
type NoticeProps struct {
	ComponentProps
	Title        string       `json:"title,omitempty"`
	Text         string       `json:"text"`
	Tone         string       `json:"tone,omitempty" enum:",danger,warning,info,ok"`
	Action       *ButtonProps `json:"action,omitempty"`
	Dismissible  bool         `json:"dismissible,omitzero"`
	DismissLabel string       `json:"dismissLabel,omitempty"`
	Live         string       `json:"live,omitempty" enum:",polite,assertive,off"`
}

// SkeletonProps defines properties for a loading placeholder. A skeleton is
// the loading rendering of content that has not arrived yet: it holds the
// geometry of the finished component so the layout does not shift when the
// real content swaps in.
type SkeletonProps struct {
	ComponentProps

	Shape string `json:"shape,omitempty"` // block, text, circle
	Size  string `json:"size,omitempty"`  // sm, md, lg
	Lines int    `json:"lines,omitempty"` // shape=text: placeholder line count (default 1)
}
