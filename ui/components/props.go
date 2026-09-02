package components

// ---- contracts/component.go
// Package contracts defines platform-agnostic Props schemas for all UI components.
//
// Contracts are data schemas (Props structs), not behavior interfaces. Existing
// builders gain an optional Props() method. Build() remains the primary API.
// This package is purely additive — zero breaking changes.
//
// Consumers:
//   - Go developers: import Props types for type-safe construction
//   - registry.ComponentDefinition: LLM-facing JSON schema (A2UI/MCP)
//   - Future renderers (iOS, Android): implement rendering from Props

// ComponentProps is the base set of properties shared by all components.
type ComponentProps struct {
	// ID is supplied by the component-tree transport, not authored as a
	// component property in design manifests or A2UI schemas.
	ID string `json:"id,omitempty" delivery:"internal"`
	// Class is a renderer escape hatch for trusted Go composition. It is not a
	// portable design-system property and is therefore excluded from delivery.
	Class    string `json:"class,omitempty" delivery:"internal"`
	Disabled bool   `json:"disabled,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
	// Attrs is restricted to trusted direct-render callers; portable contracts
	// expose explicit typed properties instead of arbitrary HTML attributes.
	Attrs map[string]string `json:"attrs,omitempty" delivery:"internal"`
}

// HTMXProps contains HTMX-specific properties for server-driven interactions.
type HTMXProps struct {
	Get         string `json:"hx-get,omitempty"`
	Post        string `json:"hx-post,omitempty"`
	Put         string `json:"hx-put,omitempty"`
	Patch       string `json:"hx-patch,omitempty"`
	Delete      string `json:"hx-delete,omitempty"`
	Target      string `json:"hx-target,omitempty"`
	Swap        string `json:"hx-swap,omitempty"`
	Trigger     string `json:"hx-trigger,omitempty"`
	Include     string `json:"hx-include,omitempty"`
	Confirm     string `json:"hx-confirm,omitempty"`
	Ext         string `json:"hx-ext,omitempty"`
	Indicator   string `json:"hx-indicator,omitempty"`
	DisabledElt string `json:"hx-disabled-elt,omitempty"`
	Vals        string `json:"hx-vals,omitempty"`
	PushURL     string `json:"hx-push-url,omitempty"`
	Select      string `json:"hx-select,omitempty"`
	Boost       bool   `json:"hx-boost,omitempty"`
	Disable     bool   `json:"hx-disable,omitempty"`
}

// ---- contracts/atoms/button.go
// ButtonProps defines the platform-agnostic properties for a Button component.
type ButtonProps struct {
	ComponentProps
	HTMXProps

	Label     string `json:"label"`
	Href      string `json:"href,omitempty"`    // renders an anchor with button styling when set
	Variant   string `json:"variant,omitempty"` // primary, secondary, outline, ghost, link
	Tone      string `json:"tone,omitempty"`    // neutral, brand, success, warning, danger, info
	Size      string `json:"size,omitempty"`    // xs, sm, md, lg, xl, 2xl
	Type      string `json:"type,omitempty"`    // button, submit, reset
	Loading   bool   `json:"loading,omitempty"`
	FullWidth bool   `json:"fullWidth,omitempty"`
	IconOnly  bool   `json:"iconOnly,omitempty"`
	AriaLabel string `json:"ariaLabel,omitempty"`
}

// ---- contracts/atoms/badge.go
// BadgeProps defines the platform-agnostic properties for a Badge component.
type BadgeProps struct {
	ComponentProps

	Label       string `json:"label"`
	Variant     string `json:"variant,omitempty"` // primary, secondary, outline
	Tone        string `json:"tone,omitempty"`    // neutral, brand, success, warning, danger, info
	Size        string `json:"size,omitempty"`    // xs, sm, md, lg, xl, 2xl
	Dot         bool   `json:"dot,omitempty"`     // show status dot before the label
	Count       int    `json:"count,omitempty"`   // positive count, visually capped to 99+
	Removable   bool   `json:"removable,omitempty"`
	RemoveLabel string `json:"removeLabel,omitempty"` // localized remove-button label
	Live        bool   `json:"live,omitempty"`        // polite status announcement
}

// ---- contracts/atoms/alert.go
// AlertProps defines properties for a persistent inline status message.
type AlertProps struct {
	ComponentProps

	Message     string `json:"message"`
	Title       string `json:"title,omitempty"`
	Tone        string `json:"tone,omitempty"` // neutral, info, success, warning, danger (default info)
	Dismissible bool   `json:"dismissible,omitempty"`
	Bordered    bool   `json:"bordered,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
}

// ---- contracts/atoms/input.go
// InputProps defines the platform-agnostic properties for an Input component.
type InputProps struct {
	ComponentProps
	HTMXProps

	Name         string `json:"name"`
	Type         string `json:"type,omitempty"` // text, email, password, number, tel, url, search, date, time
	Value        string `json:"value,omitempty"`
	Placeholder  string `json:"placeholder,omitempty"`
	Label        string `json:"label,omitempty"`
	HelpText     string `json:"helpText,omitempty"`
	Error        string `json:"error,omitempty"`
	Invalid      bool   `json:"invalid,omitempty"`
	Required     bool   `json:"required,omitempty"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
	AutoFocus    bool   `json:"autoFocus,omitempty"`
	Min          string `json:"min,omitempty"`
	Max          string `json:"max,omitempty"`
	Step         string `json:"step,omitempty"`
	MinLength    int    `json:"minLength,omitempty"`
	MaxLength    int    `json:"maxLength,omitempty"`
	Pattern      string `json:"pattern,omitempty"`
	Autocomplete string `json:"autocomplete,omitempty"`
	Size         string `json:"size,omitempty"` // sm, md, lg
	Tone         string `json:"tone,omitempty"` // neutral, success, warning, danger
	FullWidth    bool   `json:"fullWidth,omitempty"`
}

// ---- contracts/atoms/select.go
// SelectProps defines platform-agnostic properties for a native single-value
// Select component. It mirrors InputProps where the concepts overlap so form
// builders can treat text-like and choice-like fields uniformly.
type SelectProps struct {
	ComponentProps
	HTMXProps

	Name        string         `json:"name"`
	Label       string         `json:"label,omitempty"`
	Value       string         `json:"value,omitempty"`
	Values      []string       `json:"values,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"` // rendered as a disabled-free empty option
	Options     []SelectOption `json:"options"`
	Required    bool           `json:"required,omitempty"`
	Multiple    bool           `json:"multiple,omitempty"`
	VisibleRows int            `json:"visibleRows,omitempty"`
	FullWidth   bool           `json:"fullWidth,omitempty"`
	HelpText    string         `json:"helpText,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// SelectOption is one choice in a Select.
type SelectOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// ---- contracts/atoms/textarea.go
// TextareaProps defines properties for a multi-line text input.
type TextareaProps struct {
	ComponentProps
	HTMXProps

	Name         string `json:"name"`
	Placeholder  string `json:"placeholder,omitempty"`
	Value        string `json:"value,omitempty"`
	Label        string `json:"label,omitempty"`
	HelperText   string `json:"helperText,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	Required     bool   `json:"required,omitempty"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
	Rows         int    `json:"rows,omitempty"`
	MinRows      int    `json:"minRows,omitempty"`
	MaxRows      int    `json:"maxRows,omitempty"`
	MinLength    int    `json:"minLength,omitempty"`
	MaxLength    int    `json:"maxLength,omitempty"`
	ShowCount    bool   `json:"showCount,omitempty"`
	AutoResize   bool   `json:"autoResize,omitempty"`
	FullWidth    bool   `json:"fullWidth,omitempty"`
}

// ---- contracts/atoms/form_controls.go
// CheckboxProps defines properties for a checkbox input.
type CheckboxProps struct {
	ComponentProps

	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Checked       bool   `json:"checked,omitempty"`
	Indeterminate bool   `json:"indeterminate,omitempty"`
	Value         string `json:"value,omitempty"`
	Required      bool   `json:"required,omitempty"`
	HelpText      string `json:"helpText,omitempty"`
}

// ---- contracts/atoms/text.go
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
	Level    int    `json:"level"`            // 1-6
	Anchor   string `json:"anchor,omitempty"` // optional anchor ID
	Truncate bool   `json:"truncate,omitempty"`
}

// LabelProps defines properties for form labels.
type LabelProps struct {
	ComponentProps

	Text     string `json:"text"`
	For      string `json:"for,omitempty"` // associated input ID
	Required bool   `json:"required,omitempty"`
}

// ---- contracts/atoms/visual.go
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

// ---- contracts/atoms/feedback.go
// SpinnerProps defines properties for a loading spinner.
type SpinnerProps struct {
	ComponentProps

	Label string `json:"label,omitempty"` // sr-only text
	Size  string `json:"size,omitempty"`  // xs, sm, md, lg, xl, 2xl
	Tone  string `json:"tone,omitempty"`  // neutral, brand, success, warning, danger, info (default brand)
}

// SkeletonProps lives in skeleton.go alongside DeferredSlotProps: the earlier
// contract-only draft here (free-string width/height) predated the audited
// class pipeline and had no renderer or consumers.

// ---- contracts/atoms/empty_state.go
// EmptyStateProps defines properties for an empty data state placeholder.
type EmptyStateProps struct {
	ComponentProps

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
	Bordered    bool   `json:"bordered,omitempty"`
}

// ---- contracts/atoms/skeleton.go
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

// DeferredSlotProps defines a placeholder region that HTMX replaces with a
// server-rendered fragment. Get names the fragment URL; Trigger defaults to
// "load" and Swap to "outerHTML", so the fragment replaces the slot (and its
// skeleton) wholesale with zero client code.
type DeferredSlotProps struct {
	ComponentProps
	HTMXProps
}

// ---- contracts/layouts/layouts.go
// GridProps defines properties for a CSS Grid layout.
type GridProps struct {
	ComponentProps

	Columns string `json:"columns,omitempty"` // Tailwind grid-cols value
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

// ---- contracts/molecules/data.go
// TableProps defines platform-agnostic properties for a Table component.
type TableProps struct {
	ComponentProps
	HTMXProps

	Columns    []TableColumn `json:"columns"`
	Rows       []TableRow    `json:"rows,omitempty"`
	Sortable   bool          `json:"sortable,omitempty"`
	Selectable bool          `json:"selectable,omitempty"`
	Striped    bool          `json:"striped,omitempty"`
	Compact    bool          `json:"compact,omitempty"`
	EmptyText  string        `json:"emptyText,omitempty"`
}

// TableColumn defines a table column.
type TableColumn struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Sortable bool   `json:"sortable,omitempty"`
	Primary  bool   `json:"primary,omitempty"` // emphasized identity cell
	Width    string `json:"width,omitempty"`
	Align    string `json:"align,omitempty"` // left, center, right
}

// TableRow represents a table data row.
type TableRow struct {
	ID    string         `json:"id,omitempty"`
	Cells map[string]any `json:"cells"`
}

// DetailListProps defines a compact semantic description list. Title and
// description are visible section copy; SemanticRole is a stable,
// non-localized machine key that lets adaptive renderers preserve section
// meaning without interpreting translated labels.
type DetailListProps struct {
	ComponentProps

	Title        string       `json:"title,omitempty"`
	Description  string       `json:"description,omitempty"`
	SemanticRole string       `json:"semanticRole,omitempty"`
	Items        []DetailItem `json:"items"`
}

// DetailItem is one label/value fact in a DetailList.
type DetailItem struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Tone        string `json:"tone,omitempty"` // neutral, brand, success, warning, danger, info
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
	Clickable     bool   `json:"clickable,omitempty"`
	Hoverable     bool   `json:"hoverable,omitempty"`
	Href          string `json:"href,omitempty"`
}

// ModalProps defines platform-agnostic properties for a Modal component.
type ModalProps struct {
	ComponentProps

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Footer      string `json:"footer,omitempty"`
	AriaLabel   string `json:"ariaLabel,omitempty"`
	CloseLabel  string `json:"closeLabel,omitempty"`
	Size        string `json:"size,omitempty"` // small, medium, large, xl, full

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

// SidebarProps defines properties for a sidebar navigation.
type SidebarProps struct {
	ComponentProps

	Items           []SidebarItem    `json:"items,omitempty"`
	Sections        []SidebarSection `json:"sections,omitempty"`
	Current         string           `json:"current,omitempty"`
	Flavor          string           `json:"flavor,omitempty"` // admin, content
	Collapsible     bool             `json:"collapsible,omitempty"`
	Collapsed       bool             `json:"collapsed,omitempty"`
	NavigationLabel string           `json:"navigationLabel,omitempty"`
	BrandLabel      string           `json:"brandLabel,omitempty"`
	BrandHref       string           `json:"brandHref,omitempty"`
}

// SidebarItem represents a sidebar navigation item.
type SidebarItem struct {
	ID           string            `json:"id,omitempty"`
	Label        string            `json:"label"`
	Href         string            `json:"href,omitempty"`
	Icon         string            `json:"icon,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`
	Badge        string            `json:"badge,omitempty"`
	BadgeVariant string            `json:"badgeVariant,omitempty"`
	Active       bool              `json:"active,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
	SearchText   string            `json:"searchText,omitempty"`
	Attrs        map[string]string `json:"-" delivery:"internal"`
	Children     []SidebarItem     `json:"children,omitempty"`
}

// SidebarSection groups related sidebar items under an optional heading.
type SidebarSection struct {
	ID         string        `json:"id,omitempty"`
	Label      string        `json:"label,omitempty"`
	Glyph      string        `json:"glyph,omitempty"`
	Tone       string        `json:"tone,omitempty"` // neutral, brand, success, warning, danger, info
	SearchText string        `json:"searchText,omitempty"`
	Items      []SidebarItem `json:"items,omitempty"`
}

// Option represents a selectable option.
type Option struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Group       string `json:"group,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// ---- contracts/molecules/navigation.go
// TabsProps defines properties for a tabbed interface.
type TabsProps struct {
	ComponentProps

	Items        []TabItem `json:"items,omitempty"`
	ActiveTab    string    `json:"activeTab,omitempty"`
	Orientation  string    `json:"orientation,omitempty"` // horizontal, vertical
	Variant      string    `json:"variant,omitempty"`     // underline, pills
	HxGet        string    `json:"hxGet,omitempty"`       // default lazy-panel endpoint
	LoadingLabel string    `json:"loadingLabel,omitempty"`
}

// TabItem represents a tab.
type TabItem struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Icon     string `json:"icon,omitempty"`
	Badge    string `json:"badge,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Content  string `json:"content,omitempty"` // static panel content for direct rendering
	URL      string `json:"url,omitempty"`     // navigation target in item mode
	HxGet    string `json:"hxGet,omitempty"`   // lazy-panel endpoint in panel mode
}

// BreadcrumbProps defines properties for breadcrumb navigation.
type BreadcrumbProps struct {
	ComponentProps
	HTMXProps

	Items     []BreadcrumbItem `json:"items"`
	Separator string           `json:"separator,omitempty"` // default "/"
	MaxItems  int              `json:"maxItems,omitempty"`  // collapse middle items
}

// BreadcrumbItem represents a breadcrumb segment.
type BreadcrumbItem struct {
	Label   string `json:"label"`
	Href    string `json:"href,omitempty"` // empty = current page
	Icon    string `json:"icon,omitempty"`
	Current bool   `json:"current,omitempty"`
}

// PaginationProps defines properties for pagination controls.
type PaginationProps struct {
	ComponentProps
	HTMXProps

	CurrentPage     int    `json:"currentPage"`
	TotalPages      int    `json:"totalPages"`
	PerPage         int    `json:"perPage,omitempty"`
	Siblings        int    `json:"siblings,omitempty"` // pages shown around current
	BaseURL         string `json:"baseURL,omitempty"`
	CursorMode      string `json:"cursorMode,omitempty"` // previous-next, load-more
	PreviousCursor  string `json:"previousCursor,omitempty"`
	NextCursor      string `json:"nextCursor,omitempty"`
	BeforeParameter string `json:"beforeParameter,omitempty"`
	AfterParameter  string `json:"afterParameter,omitempty"`
	PreviousURL     string `json:"previousURL,omitempty"`
	NextURL         string `json:"nextURL,omitempty"`
	PreviousLabel   string `json:"previousLabel,omitempty"`
	NextLabel       string `json:"nextLabel,omitempty"`
	LoadMoreLabel   string `json:"loadMoreLabel,omitempty"`
	NavigationLabel string `json:"navigationLabel,omitempty"`
}

// ActionMenuItem represents a single menu entry.
type ActionMenuItem struct {
	Label     string `json:"label"`
	Icon      string `json:"icon,omitempty"`
	Href      string `json:"href,omitempty"`
	Tone      string `json:"tone,omitempty"` // neutral, danger
	Danger    bool   `json:"danger,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	HxGet     string `json:"hxGet,omitempty"`
	HxPost    string `json:"hxPost,omitempty"`
	HxDelete  string `json:"hxDelete,omitempty"`
	HxTarget  string `json:"hxTarget,omitempty"`
	HxSwap    string `json:"hxSwap,omitempty"`
	HxConfirm string `json:"hxConfirm,omitempty"`
	Action    string `json:"action,omitempty"`
	// Attrs is a trusted Go-only escape hatch for stable data attributes.
	Attrs map[string]string `json:"attrs,omitempty" delivery:"internal"`
}

// ---- contracts/molecules/skeleton.go
// TableSkeletonProps defines the loading rendering of a Table: the same wrap,
// header, and cell classes with pulsing placeholders where data will land.
type TableSkeletonProps struct {
	ComponentProps

	Columns int  `json:"columns,omitempty"` // header/cell count (default 4)
	Rows    int  `json:"rows,omitempty"`    // placeholder row count (default 3)
	Compact bool `json:"compact,omitempty"`
}
