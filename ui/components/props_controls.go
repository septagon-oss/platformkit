package components

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
	Value       string `json:"value,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Label       string `json:"label,omitempty"`
	HelpText    string `json:"helpText,omitempty"`
	// HelpNotes are the sentences a form says about the control it chose, as
	// distinct from HelpText, which is what the field's author says about the
	// field. Each is drawn in its own span in the same help paragraph.
	HelpNotes    []string `json:"helpNotes,omitempty"`
	Error        string   `json:"error,omitempty"`
	Invalid      bool     `json:"invalid,omitempty"`
	Required     bool     `json:"required,omitempty"`
	ReadOnly     bool     `json:"readOnly,omitempty"`
	AutoFocus    bool     `json:"autoFocus,omitempty"`
	Min          string   `json:"min,omitempty"`
	Max          string   `json:"max,omitempty"`
	Step         string   `json:"step,omitempty"`
	MinLength    int      `json:"minLength,omitempty"`
	MaxLength    int      `json:"maxLength,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	Autocomplete string   `json:"autocomplete,omitempty"`
	Size         string   `json:"size,omitempty"` // sm, md, lg
	Tone         string   `json:"tone,omitempty"` // neutral, success, warning, danger
	FullWidth    bool     `json:"fullWidth,omitempty"`

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
	// HelpNotes are the sentences a form says about the control it chose, as
	// distinct from HelpText, which is what the field's author says about the
	// field. Each is drawn in its own span in the same help paragraph.
	HelpNotes []string `json:"helpNotes,omitempty"`
	Error     string   `json:"error,omitempty"`
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

	Name        string `json:"name"`
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
	Label       string `json:"label,omitempty"`
	HelperText  string `json:"helperText,omitempty"`
	// HelperNotes are the sentences a form says about the control it chose, as
	// distinct from HelperText, which is what the field's author says about the
	// field. Each is drawn in its own span in the same helper paragraph.
	HelperNotes  []string `json:"helperNotes,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
	Required     bool     `json:"required,omitempty"`
	ReadOnly     bool     `json:"readOnly,omitempty"`
	Rows         int      `json:"rows,omitempty"`
	MinRows      int      `json:"minRows,omitempty"`
	MaxRows      int      `json:"maxRows,omitempty"`
	MinLength    int      `json:"minLength,omitempty"`
	MaxLength    int      `json:"maxLength,omitempty"`
	// CharacterLimitOnly shows the limit without HTML's UTF-16 maxlength gate.
	CharacterLimitOnly bool `json:"characterLimitOnly,omitempty"`
	ShowCount          bool `json:"showCount,omitempty"`
	AutoResize         bool `json:"autoResize,omitempty"`
	FullWidth          bool `json:"fullWidth,omitempty"`
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
	// HelpNotes are the sentences a form says about the control it chose, as
	// distinct from HelpText, which is what the field's author says about the
	// field. Each is drawn in its own span in the same help paragraph.
	HelpNotes []string `json:"helpNotes,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// LabelProps defines properties for form labels.
type LabelProps struct {
	ComponentProps

	Text     string `json:"text"`
	For      string `json:"for,omitempty"` // associated input ID
	Required bool   `json:"required,omitempty"`
}
