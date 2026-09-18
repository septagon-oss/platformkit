package components

// Props are the typed inputs to the component constructors in this package.
// Example captures those same inputs and derives portable property schemas;
// screens.FormExample uses that path for generated entity forms, and export.Export
// carries the captured contracts to design consumers. See example.go.
//
// These contracts describe presentation. A2UI/MCP transport and native renderers
// require their own adapters; neither protocol processing nor entity persistence
// is implemented by a Props schema.

// ComponentProps is the base set of properties shared by all components.
type ComponentProps struct {
	// ID is a DOM identity supplied by trusted Go composition. Portable property
	// schemas omit it; captured source occurrences use ExampleInfo.ID separately.
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

// ButtonProps defines the platform-agnostic properties for a Button component.
type ButtonProps struct {
	ComponentProps
	HTMXProps

	Label     string `json:"label"`
	Href      string `json:"href,omitempty"`                                                   // renders an anchor with button styling when set
	Variant   string `json:"variant,omitempty" enum:",primary,secondary,outline,ghost,link"`   // primary, secondary, outline, ghost, link
	Tone      string `json:"tone,omitempty" enum:",neutral,brand,success,warning,danger,info"` // neutral, brand, success, warning, danger, info
	Size      string `json:"size,omitempty" enum:",xs,sm,md,lg,xl,2xl"`                        // xs, sm, md, lg, xl, 2xl
	Type      string `json:"type,omitempty" enum:",button,submit,reset"`                       // button, submit, reset
	Loading   bool   `json:"loading,omitempty"`
	FullWidth bool   `json:"fullWidth,omitempty"`
	IconOnly  bool   `json:"iconOnly,omitempty"`
	AriaLabel string `json:"ariaLabel,omitempty"`
}

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
	Bordered     bool   `json:"bordered,omitempty"`
	Compact      bool   `json:"compact,omitempty"`
}

// InputProps defines the platform-agnostic properties for an Input component.
type InputProps struct {
	ComponentProps
	HTMXProps

	Name string `json:"name"`
	// A hidden input keeps its native form value while hiding the entire field,
	// including any label, icons and supporting text, from layout and focus.
	Type string `json:"type,omitempty"` // text, email, password, number, tel, url, search, date, time, file, hidden
	// Value is what the control starts with. A file input never carries one:
	// no browser lets a page choose a file for somebody.
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

	// Accept and Multiple belong to Type "file" and are ignored elsewhere.
	// Accept is the browser's filter — "image/*", or a comma-separated list of
	// extensions and media types — and it is a courtesy to the person choosing,
	// never a check: what a form actually accepts is decided by whatever reads
	// the upload. Multiple lets them choose more than one.
	Accept   string `json:"accept,omitempty"`
	Multiple bool   `json:"multiple,omitempty"`
}

// SelectProps defines platform-agnostic properties for native single or multiple
// selection. It mirrors InputProps where the concepts overlap so form
// builders can treat text-like and choice-like fields uniformly.
type SelectProps struct {
	ComponentProps
	HTMXProps

	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	// Values are exact option identifiers, not display labels. Value's empty
	// zero value leaves selection to the placeholder/browser; Values can name
	// an explicit empty option, including in a multiple selection.
	Value       string         `json:"value,omitempty"`
	Values      []string       `json:"values,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"` // a named, disabled empty option in single-selection mode
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
	Error         string `json:"error,omitempty"`
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

// LabelProps defines properties for form labels.
type LabelProps struct {
	ComponentProps

	Text     string `json:"text"`
	For      string `json:"for,omitempty"` // associated input ID
	Required bool   `json:"required,omitempty"`
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

// SkeletonProps lives in skeleton.go: the earlier
// contract-only draft here (free-string width/height) predated the audited
// class pipeline and had no renderer or consumers.

// EmptyStateProps defines properties for an empty data state placeholder.
type EmptyStateProps struct {
	ComponentProps

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
	Bordered    bool   `json:"bordered,omitempty"`
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
	// NavigationLabel names the landmark. Empty keeps "Breadcrumb".
	NavigationLabel string `json:"navigationLabel,omitempty"`
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

	CurrentPage int `json:"currentPage"`
	TotalPages  int `json:"totalPages"`
	// Siblings is how many pages are shown either side of the current one.
	Siblings int    `json:"siblings,omitempty"`
	BaseURL  string `json:"baseURL,omitempty"`
	// NavigationLabel names this landmark, for a page carrying more than one.
	NavigationLabel string `json:"navigationLabel,omitempty"`
	// PreviousLabel and NextLabel name the boundary controls for assistive
	// technology. Empty keeps "Previous page" and "Next page".
	PreviousLabel string `json:"previousLabel,omitempty"`
	NextLabel     string `json:"nextLabel,omitempty"`
	// PageLabel and CurrentPageLabel name the numbered links; %d stands for the
	// page number. Empty keeps "Go to page %d" and "Page %d, current page".
	PageLabel        string `json:"pageLabel,omitempty"`
	CurrentPageLabel string `json:"currentPageLabel,omitempty"`
}

// TableSkeletonProps defines the loading rendering of a Table: the same wrap,
// header, and cell classes with pulsing placeholders where data will land.
type TableSkeletonProps struct {
	ComponentProps

	Columns int  `json:"columns,omitempty"` // header/cell count (default 4)
	Rows    int  `json:"rows,omitempty"`    // placeholder row count (default 3)
	Compact bool `json:"compact,omitempty"`
}

// AvatarProps is a person as a disc: the thing every member row, comment and
// attendee list needs, and therefore the thing every product otherwise hand-rolls
// as a coloured div with a letter in it.
type AvatarProps struct {
	ComponentProps

	// Name is the person as they are called. It is the accessible name of the
	// disc and the source of the initials; nothing here invents a substitute for
	// a caller who left it empty.
	Name string `json:"name,omitempty" maxLength:"120"`
	// Src is their picture, when one exists. It goes through Media, so alt text,
	// lazy loading and the four ways a picture fails are decided once.
	Src string `json:"src,omitempty" maxLength:"2048"`
	// Href makes the disc a link to the person. Link is a text link and cannot
	// hold a picture, so this disc becomes the anchor itself, the way Card does.
	Href string `json:"href,omitempty" maxLength:"2048"`
	Size string `json:"size,omitempty" enum:",sm,md,lg" enumStrict:"true"`
	// Decorative says the name is already on screen beside this disc, so the disc
	// says nothing at all. Left false, the disc carries the name itself.
	Decorative bool `json:"decorative,omitempty"`
	// AriaLabel names the disc when the caller's word for the person is not the
	// name to show — "Signed out", or a role where a name would be a claim.
	AriaLabel string `json:"ariaLabel,omitempty"`
}

// MediaStatus is where a picture has got to. It exists because a picture that
// has not arrived yet, a picture that never will, and a picture somebody is not
// allowed to see all look the same — a broken-image glyph — to the only renderer
// this repository had before: Card emitted an <img> whatever it knew.
//
// The five names are the shared view-state vocabulary, not a media invention:
// the same words carry a list, a trail and a cut-out, so a screen does not say
// "no results" about a refusal and "error" about an empty shelf. A capability
// that owns a different lifecycle maps onto these five at its own edge and says
// which words it used; nothing below this package invents a sixth.
type MediaStatus string

const (
	// MediaReady renders the picture. An empty Status means ready, because
	// everything that has a source has one to show.
	MediaReady MediaStatus = "ready"
	// MediaLoading holds the box the picture will land in, and says so to a
	// screen reader rather than leaving it guessing at a rectangle.
	MediaLoading MediaStatus = "loading"
	// MediaEmpty is "there is nothing here", which is a fact about the shelf,
	// not about the viewer.
	MediaEmpty MediaStatus = "empty"
	// MediaFailed is "we could not get it", with the reason in the capability's
	// own words. It is not empty: an error that renders as an empty state loses
	// the one thing a caller needs to hear.
	MediaFailed MediaStatus = "failed"
	// MediaRefused is "you may not see this". Distinct from empty on purpose:
	// hiding a permission behind "nothing here" teaches everybody that the
	// product lies, and it is the difference between asking for access and
	// going to look somewhere else.
	MediaRefused MediaStatus = "refused"
)

// MediaProps defines one picture and what to say about it while it is not there.
//
// Alt is required unless Decorative is true: a picture that carries information
// with no text alternative is invisible to exactly the people the rest of this
// package is written for. The renderer cannot read the picture to find out, so
// the requirement is stated here, every specimen in the gallery obeys it, and
// e2e/design-audit.spec.ts measures it on the painted page — where it belongs,
// because a rule only in a comment is a preference.
type MediaProps struct {
	ComponentProps

	Status MediaStatus `json:"status,omitempty"`
	Src    string      `json:"src,omitempty"`
	Alt    string      `json:"alt,omitempty"`
	// Decorative marks a picture that says nothing an assistive reader would
	// miss, which is the only honest way to arrive at an empty alt.
	Decorative bool `json:"decorative,omitempty"`
	// Width and Height are the picture's intrinsic size in pixels. They are
	// attributes, not styling: without them the box is 0px tall until the bytes
	// land, and the page underneath jumps when they do.
	// Each on its own line: a struct tag written after a shared field line is
	// shared too, so this as one declaration gave Height the json name "width"
	// and the two collided — which the gallery's own description test caught,
	// and no compiler would have.
	Width   int    `json:"width,omitempty" minimum:"0"`
	Height  int    `json:"height,omitempty" minimum:"0"`
	Caption string `json:"caption,omitempty"`
	// Reason is the capability's own sentence for a failure or a refusal. The
	// renderer holds no words of its own: "the cut-out failed" and "the print
	// shop is closed" are the same state and different news.
	Reason string `json:"reason,omitempty"`
	// Horizontal is a fixed-width thumbnail beside text rather than a full-width
	// image above it. It is one field and not a class string, because the two
	// forms differ in one width and this is the picture Card has always drawn:
	// cardImage delegates here, so there is one place a picture's markup lives.
	Horizontal bool `json:"horizontal,omitempty"`
	// Lazy defers the fetch until the picture is near the viewport, which is what
	// a long list of them wants. Left unset it loads eagerly, because a picture
	// above the fold that waits for a scroll is a picture nobody asked to wait for.
	Lazy bool `json:"lazy,omitempty"`
	// Fit is what the box does with a picture of another shape: "cover" crops to
	// fill it, "contain" keeps the whole subject inside. It is the caller's call
	// because it is a claim about the picture — a face cropped is a different
	// picture — and not about the box. Empty leaves the source's own aspect.
	Fit string `json:"fit,omitempty" enum:",contain,cover" enumStrict:"true"`
}
