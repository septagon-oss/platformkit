package components

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
	// DisclosureLabel names SidebarDisclosure's summary below the large breakpoint ("Menu" when empty).
	DisclosureLabel string `json:"disclosureLabel,omitempty"`
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
