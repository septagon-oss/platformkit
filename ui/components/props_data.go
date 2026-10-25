package components

import "time"

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

	// Label names the scroll wrapper so a table wider than its box can be read
	// with a keyboard alone. The wrapper is `overflow: auto`, and a scroll box
	// that cannot take focus scrolls only for a pointer: the columns past the
	// right edge exist in the DOM and are unreachable for a person who never
	// touches a mouse (WCAG 2.1.1). Making it focusable without naming it would
	// add a landmark a screen reader announces as "region" with nothing to
	// identify it, eleven times on a dashboard. So the wrapper becomes a named
	// region when a caller supplies a name, and stays exactly what it was when
	// nobody does. There is deliberately no default: an invented name would be
	// the renderer claiming to know what its caller's table is about.
	Label string `json:"label,omitempty"`
}

// TableColumn defines a table column.
type TableColumn struct {
	RowHeader bool   `json:"rowHeader,omitzero"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	Sortable  bool   `json:"sortable,omitempty"`
	Primary   bool   `json:"primary,omitempty"` // emphasized identity cell
	Width     string `json:"width,omitempty"`
	Align     string `json:"align,omitempty"` // left, center, right
	// Stacked folds the column into its row below the sm breakpoint: its header
	// and cells are not drawn there; the row's first unstacked cell lists its
	// label and value, and that column's header its sort control, so a phone
	// reads and sorts every field without scrolling the table sideways. At sm
	// and up it is an ordinary column.
	Stacked bool `json:"stacked,omitzero"`
}

// TableRow represents a table data row.
type TableRow struct {
	ID    string         `json:"id,omitempty"`
	Cells map[string]any `json:"cells"`
}

// DataRow retains Table's value projection and supplies optional native controls.
type DataRow struct {
	TableRow
	Href           string `json:"href,omitempty"`
	Selectable     bool   `json:"selectable,omitzero"`
	SelectionLabel string `json:"selectionLabel,omitempty"`
	Revision       string `json:"revision,omitempty"` // passed to the caller's RowActions; never interpreted
	StatusLabel    string `json:"statusLabel,omitempty"`
	Tone           string `json:"tone,omitempty"`
}

// DataGroup's CountText is the supplied total, not the number of loaded rows.
type DataGroup struct {
	Key         string    `json:"key"`
	Title       string    `json:"title,omitempty"`
	CountText   string    `json:"countText,omitempty"`
	Rows        []DataRow `json:"rows,omitempty"`
	Collapsible bool      `json:"collapsible,omitzero"`
	Collapsed   bool      `json:"collapsed,omitzero"`
}

// DataListProps composes tables without owning queries, saved views or bulk writes.
// DataListLoading describes public geometry without retaining a previous result.
type DataListLoading struct {
	Rows         int  `json:"rows"`
	GroupHeading bool `json:"groupHeading,omitzero"`
	BulkAction   bool `json:"bulkAction,omitzero"`
}

type DataListProps struct {
	ComponentProps
	HTMXProps
	Label                string           `json:"label"`
	State                ContentState     `json:"state,omitzero"`
	Columns              []TableColumn    `json:"columns,omitempty"`
	Groups               []DataGroup      `json:"groups,omitempty"`
	Filters              []ChoiceLink     `json:"filters,omitempty"`
	SortChoices          []ChoiceLink     `json:"sortChoices,omitempty"`
	Views                []ChoiceLink     `json:"views,omitempty"`
	ResultKey            string           `json:"resultKey,omitempty"`
	SelectedIDs          []string         `json:"selectedIDs,omitempty"`
	SelectionName        string           `json:"selectionName,omitempty"`
	FormID               string           `json:"formID,omitempty"`
	SelectAllLabel       string           `json:"selectAllLabel,omitempty"`
	ClearSelectionLabel  string           `json:"clearSelectionLabel,omitempty"`
	SelectionCountLabels []string         `json:"selectionCountLabels,omitempty"`
	ResultCountText      string           `json:"resultCountText,omitempty"`
	Compact              bool             `json:"compact,omitzero"`
	Pagination           *PaginationProps `json:"pagination,omitempty"`
	LoadingLayout        *DataListLoading `json:"loadingLayout,omitempty"`
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

// DetailPanelProps carries an authorized item snapshot or a cleared ContentState.
type DetailPanelProps struct {
	ComponentProps
	ItemID      string       `json:"itemID,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Label       string       `json:"label"`
	State       ContentState `json:"state"`
	CloseLabel  string       `json:"closeLabel"`
	ReturnHref  string       `json:"returnHref"`
	Size        string       `json:"size,omitempty"`
	Busy        bool         `json:"busy,omitzero"`
}

// TimeText keeps the machine instant separate from caller-localized display copy.
type TimeText struct {
	AtUTC time.Time `json:"atUTC"`
	Text  string    `json:"text"`
}

type Change struct {
	Label        string `json:"label"`
	BeforeText   string `json:"beforeText,omitempty"`
	AfterText    string `json:"afterText,omitempty"`
	Redacted     bool   `json:"redacted,omitzero"`
	RedactedText string `json:"redactedText,omitempty"`
}

type TimelineItem struct {
	ID          string       `json:"id"`
	ActorText   string       `json:"actorText"`
	ActorAvatar *AvatarProps `json:"actorAvatar,omitempty"`
	Time        TimeText     `json:"time"`
	Summary     string       `json:"summary"`
	StatusLabel string       `json:"statusLabel,omitempty"`
	Tone        string       `json:"tone,omitempty"`
	Changes     []Change     `json:"changes,omitempty"`
	DetailsHref string       `json:"detailsHref,omitempty"`
}

type TimelineProps struct {
	ComponentProps
	Label        string         `json:"label"`
	State        ContentState   `json:"state"`
	Items        []TimelineItem `json:"items,omitempty"`
	Layout       string         `json:"layout,omitempty"`
	BeforeLabel  string         `json:"beforeLabel"`
	AfterLabel   string         `json:"afterLabel"`
	ActorLabel   string         `json:"actorLabel"`
	TimeLabel    string         `json:"timeLabel"`
	DetailsLabel string         `json:"detailsLabel"`
	More         *ChoiceLink    `json:"more,omitempty"`
}

// TableSkeletonProps defines the loading rendering of a Table: the same wrap,
// header, and cell classes with pulsing placeholders where data will land.
type TableSkeletonProps struct {
	// Interactive reserves native 44px controls in headers and rows.
	Interactive bool `json:"interactive,omitzero"`
	ComponentProps

	Columns int  `json:"columns,omitempty"` // header/cell count (default 4)
	Rows    int  `json:"rows,omitempty"`    // placeholder row count (default 3)
	Compact bool `json:"compact,omitempty"`
}
