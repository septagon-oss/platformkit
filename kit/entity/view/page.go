package view

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// PageVersion is the revision of a Page's meaning: 1 today. It moves when an
// existing member changes meaning, never for an added one, the rule
// present.PolicyVersion and the catalogue's own version live by.
const PageVersion = 1

// SupportedVersion reports whether this build reads a page written at version v. A
// build reads exactly the revision it was compiled against: a newer page is refused
// whole rather than partly understood, and an older one is not upgraded by guessing
// what an absent member meant.
func SupportedVersion(v int) bool { return v == PageVersion }

// Kind is which surface the page shows: a list of rows, one record, or one form — the
// whole supported set, the three shapes the generated screens answer today. A
// commands-only resource has no page: what it offers reaches a person as the actions of
// whatever page the shell is on. A command's result is a KindRecord page over the
// fields it returned, which is why Action.HasResult exists and no result kind does.
type Kind string

// The three surfaces a Page can describe.
const (
	KindList   Kind = "list"
	KindRecord Kind = "record"
	KindForm   Kind = "form"
)

// Page is one screen of one resource for one caller, as the platform decides it and
// any shell paints it. Every member is always written: the only nulls in a Page are
// field values, "" and [] mean "none", and no member is ever absent. It carries no
// address — which route a verb posts to and which screen a resource reads belong to
// the painter, resolved from the catalogue entry the shell already holds. Sections,
// Table and Form each carry only what Kind says this page shows; Empty is a list's
// word for no rows and "" elsewhere; BuiltAt is State.Now in UTC, the instant a
// relative phrase is anchored to, phrasing being painting and present.Ago the unit
// and number that painting is made of.
type Page struct {
	PageVersion int       `json:"pageVersion"`
	Kind        Kind      `json:"kind"`
	BuiltAt     time.Time `json:"builtAt"`
	Language    string    `json:"language"` // the policy's resolved tag
	Zone        string    `json:"zone"`     // State.Zone's name, "UTC" when none was given
	// Ref names what the page is about: the record, or the collection.
	Ref         Ref       `json:"ref"`
	Title       string    `json:"title"`
	Breadcrumbs []Crumb   `json:"breadcrumbs"`
	Empty       string    `json:"empty"`
	Actions     []Action  `json:"actions"`
	Sections    []Section `json:"sections"`
	Table       Table     `json:"table"`
	Form        Form      `json:"form"`
}

// Ref names a record semantically — the resource and its id — so a shell names a
// control after it and survives a row reorder and a translation. Resource is
// `module/entity`, as a field's declared reference carries it, and "" only for a page
// nobody registered. ID is exactly as the row's own `id` reads: "" for a list page, a
// create form, and a tenant's single row, which has no id of its own to send.
type Ref struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
}

// Crumb is one step of the path back. Ref is the zero Ref for the workspace root, a
// Resource with a blank ID for the collection, and the record for the page itself.
type Crumb struct {
	Label string `json:"label"`
	Ref   Ref    `json:"ref"`
}

// Window is the slice of a collection this page shows, as the read asked for it, and
// is always written: a default the contract hides is one a second shell guesses
// differently. Limit is the caller's own page size, never this package's, which keeps
// none, and it is 0 where the page's kind shows no table: no read happened there, so no
// page size was asked for.
type Window struct {
	Offset int `json:"offset"` // rows skipped, >= 0
	Limit  int `json:"limit"`  // rows asked for, > 0 on a list page, 0 where no table shows
}

// Table is a list: its columns, rows and window, its counts, and the orders and
// narrowings it offers. Sorts, Filters, DefaultSort and every column label are copies
// of the resolved policy, never recomputed, so a page and the catalogue document
// cannot disagree about one list. Total means nothing unless Counted.
type Table struct {
	Columns     []Column                     `json:"columns"`
	Rows        []Row                        `json:"rows"`
	Window      Window                       `json:"window"`
	Total       int64                        `json:"total"`
	Counted     bool                         `json:"counted"` // false: the read counted nothing, so print no count
	Sort        string                       `json:"sort"`    // the exact `sort` the read used, "" when none was sent
	DefaultSort string                       `json:"defaultSort"`
	Sorts       []present.ResolvedSortOption `json:"sorts"`
	Filters     []present.ResolvedFilter     `json:"filters"`
}

// Column is one header. Sort names the order a second click asks for in the grammar
// the screens use today — the field ascending, or "-field" — and "" when the column
// offers no order; Current says this column is what Table.Sort orders by.
type Column struct {
	Field    string `json:"field"`
	Label    string `json:"label"`
	Sortable bool   `json:"sortable"`
	Sort     string `json:"sort"`
	Current  bool   `json:"current"`
}

// Row is one record in the table. Cells holds one cell per column, in column order,
// so a row's length is always len(Table.Columns) and no shell looks a cell up by name.
type Row struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Cells []Cell `json:"cells"`
}

// Cell is one field of one row: the same six readings a record's Field carries,
// spelled the same way, so a value never means two things in two places.
type Cell struct {
	Field     string    `json:"field"`
	Label     string    `json:"label"`
	Text      string    `json:"text"`
	Tone      string    `json:"tone"`
	ValueKind ValueKind `json:"valueKind"`
	Elem      ValueKind `json:"elem"`
	Value     any       `json:"value"`
}

// Section is one block of a record. Key is "overview" for the status-and-summary
// block, a declared entity section key for a declared block, and "information" for the
// fields belonging to no block; Label is the declared label and "" for the two derived
// ones. Membership comes from the resolved layout, never re-derived, and a block with
// no fields is not written.
type Section struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}

// Field is one field as a record shows it. Reference names the resource a value
// points at, copied from the policy, so a shell draws a link target semantically
// without the catalogue.
type Field struct {
	Name      string    `json:"name"`
	Label     string    `json:"label"`
	Help      string    `json:"help"`
	Text      string    `json:"text"`
	Tone      string    `json:"tone"`
	Reference string    `json:"reference"`
	ValueKind ValueKind `json:"valueKind"`
	Elem      ValueKind `json:"elem"`
	Value     any       `json:"value"`
}

// Control is one field a form asks about, or one argument a command takes. It carries
// no violation: that fact lives once, in Form.Violations, joined by field name.
// ReadOnly marks a field a command owns, shown and never typed; MaxLength and Default
// are the schema's; Value is what the control starts with, the row's value or the
// default; Options is [] for a field that is not an enum; Keys is nil except for a map
// argument, whose declared keys a form draws one box for each.
type Control struct {
	Name      string                   `json:"name"`
	Label     string                   `json:"label"`
	Help      string                   `json:"help"`
	Control   string                   `json:"control"` // the widget name the resolved field names
	ReadOnly  bool                     `json:"readOnly"`
	Required  bool                     `json:"required"`
	MaxLength int                      `json:"maxLength"`
	Default   string                   `json:"default"`
	ValueKind ValueKind                `json:"valueKind"`
	Elem      ValueKind                `json:"elem"`
	Value     any                      `json:"value"`
	Options   []present.ResolvedOption `json:"options"`
	Keys      []entity.MapKey          `json:"keys"`
}

// Violation is one field's refusal sentence, keyed by the field's own name.
type Violation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Form is one create or update surface. Violations is ordered lexicographically by
// field name, so a refused write reads the same top-down in every shell; Detail is the
// writer's own sentence and "" when the page shows none, a form with no verdict being a
// blank form. ExpectedRevision is the row's own write counter (entity.RevisionField)
// the page was drawn from, which a shell sends back as entity.RevisionCondition; 0
// means the read carried none, which is what a create page always carries: it is about
// a row that does not exist yet.
type Form struct {
	Create           bool        `json:"create"`
	Title            string      `json:"title"`
	Controls         []Control   `json:"controls"`
	Violations       []Violation `json:"violations"`
	Detail           string      `json:"detail"`
	ExpectedRevision int64       `json:"expectedRevision"`
}

// Action is one thing the caller may do from this page. Verb is what to send: the
// command's verb for kind "command", the operation name for a CRUD door. Label is the
// command's own resolved word and "" for a CRUD door: this package introduces no copy,
// so "New %s" is the shell's phrase, composed from Kind and the policy's singular
// name. Target is what the action acts on — the collection for scope "collection",
// this record for scope "record". Undo names the verb that undoes this one, "" when
// nothing does: the policy carries neither that nor HasResult. Confirm is zero when
// the command asks nothing first. Inputs is always written: [] for a CRUD door, which
// asks for nothing, and the declared arguments for a command.
type Action struct {
	Kind        ActionKind `json:"kind"`
	Verb        string     `json:"verb"`
	Label       string     `json:"label"`
	Scope       Scope      `json:"scope"`
	Target      Ref        `json:"target"`
	Primary     bool       `json:"primary"`
	Destructive bool       `json:"destructive"`
	Undo        string     `json:"undo"`
	HasResult   bool       `json:"hasResult"`
	Confirm     Confirm    `json:"confirm"`
	Inputs      []Control  `json:"inputs"`
}

// ActionKind is which verb family an action belongs to.
type ActionKind string

// The four action kinds: the three CRUD doors and a declared command.
const (
	ActionCreate  ActionKind = "create"
	ActionUpdate  ActionKind = "update"
	ActionDelete  ActionKind = "delete"
	ActionCommand ActionKind = "command"
)

// Scope is what an action acts on. It is closed: a collection action acts on the set,
// a record action on Target's id.
type Scope string

// The two scopes an action can have.
const (
	ScopeCollection Scope = "collection"
	ScopeRecord     Scope = "record"
)

// Confirm is what a person reads first: three strings, as their author wrote them.
type Confirm struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	ConfirmLabel string `json:"confirmLabel"`
}

// Money is an amount counted in minor units. MinorUnits is a canonical decimal string,
// never a JSON number: an int64 amount must survive a client whose only number type is
// a double, in both signs. Currency is read from the sibling field the resolved reading
// names and is "" when the row carries none: the scale is a declaration, the currency
// of a value is a fact about the record.
type Money struct {
	MinorUnits string `json:"minorUnits"`
	Currency   string `json:"currency"`
	Scale      int    `json:"scale"`
}
