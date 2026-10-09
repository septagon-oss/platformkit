package entity

// hints.go is the reading vocabulary and the three levels of declaration that
// use it: what a resource's author says about how it reads, beside what a
// caller already sees about what it holds.
//
// The four lists are here because a refusal has to be against a list, and the
// layer that draws an icon or a tone cannot be imported from a kernel below the
// presentation layer (scripts/check_packages.sh). So the kernel names the words
// and a test inside ui/ binds each name to a drawing: the same arrangement as
// Widgets and ui/forms, for the same reason.

// Icons is every name an entry's `icon` may carry. They are semantic — what the
// resource is about — not ui/icon's glyph names: `person` is a resource kind,
// `user` is a drawing. The set is frozen because a name outside it has to refuse
// to mount, and a vocabulary that grew with each screen would refuse nothing.
//
// ui/icon binds it in a test there, not an import: an alias or a body exists for
// every name here, and the names the vendored set has no drawing for yet are
// held in a list in that test that can only shrink.
var Icons = []string{
	"task", "person", "people", "document", "settings", "plan", "calendar",
	"folder", "chart", "message", "money", "box", "tag", "building",
	"location", "link",
}

// Tones is every name an enum value's tone may carry. `brand` is deliberately
// absent: ui/components draws it for a component affordance, and a status is not
// an affordance. ui/components/badge binds it — every name here is a badge tone.
var Tones = []string{"neutral", "info", "success", "warning", "danger"}

// Formats is every name a field's `format` may carry: how a value is read, not
// what is typed into it. That other axis is Widgets, and `tel` there and `phone`
// here are one fact spelled twice because a control name and a reading name are
// different names — `relative` and `money` have no control to be spelled with.
var Formats = []string{"email", "phone", "url", "date", "datetime", "relative", "number", "money"}

// Visibilities is every name a field's `visibility` may carry. The empty string
// is not in it, because the empty string is what "shown" means when nobody said
// anything, and a key printed for a hint nobody wrote is not this contract.
var Visibilities = []string{"shown", "detail", "hidden"}

// ValidIcon reports whether a shell can draw the named icon. The empty string is
// valid: it means "no icon is claimed", which is every resource written before
// this name existed.
func ValidIcon(name string) bool { return validNamed(Icons, name, true) }

// ValidTone reports whether a shell colours a status with the named tone. The
// empty string is invalid, and it is the one asymmetry among the four predicates
// here: enumTones is a map, so "no tone for this value" is the key being absent
// and never an empty value. An entry whose value is "" declares two things at
// once, and the mount gate refuses it rather than draw it grey.
func ValidTone(tone string) bool { return validNamed(Tones, tone, false) }

// ValidFormat reports whether a reading exists for the named format. The empty
// string is valid: derive it from Type and Enum, which is kit/entity/display
// doing what it does today for most fields.
func ValidFormat(format string) bool { return validNamed(Formats, format, true) }

// ValidVisibility reports whether the named visibility is one a screen knows.
// The empty string is valid: it is `shown`, said by silence.
func ValidVisibility(v string) bool { return validNamed(Visibilities, v, true) }

func validNamed(list []string, name string, emptyOK bool) bool {
	if name == "" {
		return emptyOK
	}
	for _, w := range list {
		if w == name {
			return true
		}
	}
	return false
}

// EntryHints is what a resource's author says about how the resource reads.
// Every field is optional; a zero EntryHints is what every resource written
// before this type has, and it serialises to nothing at all.
//
// It is declared by a Spec, a Singleton or a module's own registration call —
// never by a struct tag, because a struct cannot name its own plural or the
// order its group sits in. It carries no JSON tags of its own: it is a
// declaration the mount gate reads, and the bytes are ui/screens' to write.
type EntryHints struct {
	Singular    string // "note"; a shell's default is Humanize(Entity)
	Plural      string // "notes"; a shell's default is Singular + "s"
	Description string
	Icon        string // a name Icons admits
	Group       *ResourceGroup
	// Order is a consumer's position within its own grouping. 0 means "no
	// opinion", and the catalogue's array order never moves for it: resources
	// are listed in registration order and a shell that groups re-sorts.
	Order            int
	PrimaryField     string
	PreviewField     string
	SummaryFields    []string
	StatusField      string
	Sections         []EntitySection
	EmptyDescription string
	Sortable         []string
}

// ResourceGroup is the navigation slot an entry names. Key is an identifier a
// shell groups by, Label is what a person reads. The prefix on the type name is
// not decoration: huma names a schema component after the Go type and ignores
// its package, so every type reaching this document shares one namespace.
//
// Neither key carries omitempty: inside a declared object every key is written,
// and the mount gate refuses the declaration that would leave one empty.
type ResourceGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// EntitySection is one named block of a record screen. A field's `section` names
// a key declared here; an unknown key refuses to mount.
type EntitySection struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// FieldHints is one field's reading. It is declared by struct tags, in derive's
// existing grammar, which is why it reaches httpx.Resource, the catalogue and
// the OpenAPI document with no new carrier: Describe1 publishes entity.Schema's
// fields as they are.
//
// It sits beside Widget, Present and Display rather than replacing any of them.
// What to type into, how to read and how much to show are three questions with
// three answers, and 0085 keeps each meaning.
type FieldHints struct {
	// Label is what a header, a control and a term call this field. Absent, the
	// reading is display.FieldLabel, which is Humanize(Name).
	Label string `json:"label,omitempty"`
	// Help is the line under a control. Absent, it is Field.Doc.
	Help string `json:"help,omitempty"`
	// Section names an EntitySection.Key of the owning entry.
	Section string `json:"section,omitempty"`
	// Visibility is `shown` (absent), `detail` or `hidden`. A reading decision
	// and nothing else: the field stays in the schema, the PATCH and the JSON.
	Visibility string `json:"visibility,omitempty"`
	// Format is a name Formats admits.
	Format string `json:"format,omitempty"`
	// EnumLabels maps an enum value to words; absent, Humanize(value).
	EnumLabels map[string]string `json:"enumLabels,omitempty"`
	// EnumTones maps an enum value to a name Tones admits; absent, `neutral`.
	EnumTones map[string]string `json:"enumTones,omitempty"`
	// Reference names the resource a value points at. Absent means no guessed
	// relationship, which is the rule 0085 keeps.
	Reference *FieldReference `json:"reference,omitempty"`
	// Money says how an int64 minor-unit amount is read.
	Money *FieldMoney `json:"money,omitempty"`
}

// FieldReference names the resource a value points at, as `module/entity`. Its
// shape refuses at mount; the target's registration is refused at boot, because
// a Spec cannot see a module mounted after it.
type FieldReference struct {
	Resource string `json:"resource"`
}

// FieldMoney says how an int64 minor-unit amount is read. CurrencyField names a
// sibling field carrying an ISO 4217 code; Scale is the minor-unit exponent, 2
// for EUR and USD, 0 for JPY. It restates what the column already means so a
// shell that did not write it can render it, and the gate refuses the shapes
// where that restatement could disagree with the data.
type FieldMoney struct {
	CurrencyField string `json:"currencyField"`
	Scale         int    `json:"scale"`
}

// CommandHints is one lifecycle route as its author describes it. Like
// EntryHints it is a declaration, not a wire type: ui/screens writes the bytes.
type CommandHints struct {
	// Label is the button's word. Absent, the shell reads the command's Summary,
	// which is what ui/screens does today because nothing else existed.
	Label string
	// Primary marks the one action a view should offer first.
	Primary bool
	// Destructive marks an action that costs something.
	Destructive bool
	// System marks a command no person is offered. ui/screens mounts no browser
	// route for it; the JSON route keeps its guard unchanged.
	System         bool
	Confirmation   *CommandConfirmation
	SuccessMessage string
}

// CommandConfirmation is the copy a person reads before the route runs. ui/resource
// refuses to invent a warning about an action whose consequence it cannot know,
// which is why declaring the consequence is three strings and not two.
type CommandConfirmation struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	ConfirmLabel string `json:"confirmLabel"`
}

// Hidden reports whether the author kept this field off every screen. It is a
// reading decision: the value is still in the schema and still in the JSON, and
// editability stays readOnly/immutable.
func (h FieldHints) Hidden() bool { return h.Visibility == "hidden" }

// OffList reports whether the field stays off a list of rows. `hidden` is off
// both screens and so off this one; `detail` is the field a record answers and a
// row does not need.
func (h FieldHints) OffList() bool { return h.Visibility == "hidden" || h.Visibility == "detail" }
