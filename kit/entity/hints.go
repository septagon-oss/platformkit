package entity

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// hints.go is the reading vocabulary and the three levels of declaration that
// use it: what a resource's author says about how it reads, beside what a
// caller already sees about what it holds.
//
// The four lists are here because a refusal has to be against a list, and the
// layer that draws an icon or a tone cannot be imported from a kernel below the
// presentation layer (scripts/check_packages.sh). So the kernel names the words
// and a test inside ui/ binds each name to a drawing: the same arrangement as
// Widgets and ui/forms, for the same reason.

import "slices"

// Icons is every name an entry's `icon` may carry. They are semantic — what the
// resource is about — not ui/icon's glyph names: `person` is a resource kind,
// `user` is a drawing. The set is frozen because a name outside it has to refuse
// to mount, and a vocabulary that grew with each screen would refuse nothing.
//
// ui/icon binds it in a test there, not an import: an alias or a body exists for
// every name here, and the names the vendored set has no drawing for yet are
// held in a list in that test that can only shrink.
//
// Deprecated: read IconNames(), which hands out a copy. This slice is the storage
// every gate in kit/rest compares an author's hint against, and one append that grew
// it in place would widen what the whole tree mounts.
var Icons = []string{
	"task", "person", "people", "document", "settings", "plan", "calendar",
	"folder", "chart", "message", "money", "box", "tag", "building",
	"location", "link",
}

// Tones is every name an enum value's tone may carry. `brand` is deliberately
// absent: ui/components draws it for a component affordance, and a status is not
// an affordance. ui/components/badge binds it — every name here is a badge tone.
//
// Deprecated: read ToneNames().
var Tones = []string{"neutral", "info", "success", "warning", "danger"}

// Formats is every name a field's `format` may carry: how a value is read, not
// what is typed into it. That other axis is Widgets, and `tel` there and `phone`
// here are one fact spelled twice because a control name and a reading name are
// different names — `relative` and `money` have no control to be spelled with.
//
// Deprecated: read FormatNames().
var Formats = []string{"email", "phone", "url", "date", "datetime", "relative", "number", "money"}

// Visibilities is every name a field's `visibility` may carry. The empty string
// is not in it, because the empty string is what "shown" means when nobody said
// anything, and a key printed for a hint nobody wrote is not this contract.
//
// Deprecated: read VisibilityNames().
var Visibilities = []string{"shown", "detail", "hidden"}

// Anchors is every clock-relative instant a declared predicate value may name.
// The server resolves one against the single instant of the response that answers
// it, so `today` is the day the person was asked about rather than the day the
// binary was built. `now` is that instant; `today` and `tomorrow` are the starts
// of consecutive UTC days.
//
// The set is frozen for the reason Icons is: a value outside it has to refuse to
// mount, and a fourth word is asked for by a product that needs one.
var Anchors = []string{"now", "today", "tomorrow"}

// IconNames, ToneNames, FormatNames and VisibilityNames are the four reading
// vocabularies above as copies: the names a hint may carry, for the gate that refuses
// another one and for the test in ui/ that binds each name to a drawing. Each call
// returns a fresh slice, so a caller that sorts or appends to the answer leaves the
// kernel's own list where it found it — the same promise events.Names gives a declared
// event list.
func IconNames() []string { return slices.Clone(Icons) }

// ToneNames is Tones as a copy. See IconNames.
func ToneNames() []string { return slices.Clone(Tones) }

// FormatNames is Formats as a copy. See IconNames.
func FormatNames() []string { return slices.Clone(Formats) }

// VisibilityNames is Visibilities as a copy. See IconNames.
func VisibilityNames() []string { return slices.Clone(Visibilities) }

// ValidAnchor reports whether a declared value names one of those instants. The
// empty string is invalid: an anchor is named by an author who meant one, and a
// value that is not an anchor is a literal in the field's own reading, which
// kit/rest's gate coerces and refuses.
func ValidAnchor(v string) bool { return validNamed(Anchors, v, false) }

// EntryHighlight is one declared sub-count: the words a card wears beside its
// total, and the predicate that says which rows it counts. Filter is never empty
// — a label with no predicate is the total wearing a label — and every predicate
// names one of this entity's own fields, so nothing here reaches a column the
// entity does not own.
//
// The label is a declared reading string and is resolved for language the way
// every other declared string is (ui/resource's hints.<module>/<entity> grammar);
// the answer to the question is a bare number in the summary document, which
// carries no words at all.
type EntryHighlight struct {
	Label  string           `json:"label"`
	Filter []EntryPredicate `json:"filter"`
}

// EntryPredicate is one condition on one field of this entity, spelled as data.
// A SQL fragment is not a declaration: it would name columns this entity does not
// own and refuse nothing at mount.
//
// Value is a string and nothing else, because a Go time written in a declaration
// is a frozen date. It is read as the field's own type — an anchor name or an
// RFC3339 instant for a timestamp, a number for a number — by the same coercion
// the list's filter parameter uses, so one table decides both questions.
type EntryPredicate struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

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
	// Session selects the typed guided read presentation.
	Session     bool
	Forms       *FormsHints
	Singular    string // "note"; a shell's default is Humanize(Entity)
	Plural      string // "notes"; a shell's default is Singular + "s"
	Description string
	Icon        string // a name Icons admits
	Group       *ResourceGroup
	// Order is a consumer's position within its own grouping. 0 means "no
	// opinion", and the catalogue's array order never moves for it: resources
	// are listed in registration order and a shell that groups re-sorts.
	Order         int
	PrimaryField  string
	PreviewField  string
	SummaryFields []string
	StatusField   string
	// CoverField names the `present:image` field a record opens on, and
	// ThumbnailField the one a list row shows. Both name a field and not a value:
	// a picture is a column like any other, and the screen decides which of the
	// record's pictures opens it. ThumbnailField defaults to CoverField at the
	// reader and is never echoed as a default — see ui/screens/hints.go on why a
	// computed default may not reach the wire.
	CoverField     string
	ThumbnailField string
	// PrimaryCommand names the verb a row's swipe runs. It is one of this
	// resource's own commands, and the one the document can check: an argument
	// a swipe cannot send is a command no row may run. See kit/rest's row gate.
	PrimaryCommand   string
	Sections         []EntitySection
	EmptyDescription string
	Sortable         []string
	Deck             *DeckHints
	// Highlight is the one sub-count a card may wear beside its total, and
	// TrendField names this entity's own timestamp field to break the last two
	// weeks into days. Both are questions about rows the list already reads, so
	// both belong to a resource that mounts a list: the gate refuses either
	// declared where no list route would answer it.
	Highlight  *EntryHighlight
	TrendField string
	// Saving declares this entry savable: one person may keep a copy of a row of
	// it. Nil is what every resource written before this field has and serialises
	// as no deviation at all — an entry nobody declared is not an entry that
	// refused, and the screens draw no save control for it.
	Saving *SavingHints
}

// SavingWords are the four words one savable pair's control shows. An empty word
// selects the fallback the resolver answers with, so a declaration says only what
// its author would say differently; the words are translated through the same
// entry-key path every other declared reading string takes.
type SavingWords struct {
	Save      string `json:"save"`
	Saved     string `json:"saved"`
	Remove    string `json:"remove"`
	ListTitle string `json:"listTitle"`
}

// SavingHints declares one entry savable: the pair of slugs that names it, the two
// published fields of the read that carry the source's own key and version, and
// the words its control shows. It carries no principal, no pressed state and no
// storage service — who is asking, whether they already kept this row and where
// the copy lives are facts of the request and of the composition that answers the
// control, never of a declaration a catalogue publishes.
type SavingHints struct {
	// Module and Kind are the lower-case slug pair the source contributes; the
	// same pair names the copy, so a hint and a contribution that disagree is a
	// wiring mistake the mount gate refuses rather than a copy nobody can find.
	Module string `json:"module"`
	Kind   string `json:"kind"`
	// KeyField names an exposed field carrying the source's canonical key, and
	// VersionField one carrying the source's own version. They name fields of the
	// read that declares them, which is why the pair is declared here and not on
	// the save command: the control posts what the record already published.
	KeyField     string      `json:"keyField"`
	VersionField string      `json:"versionField"`
	Words        SavingWords `json:"words"`
}

// savingSlug is the grammar a savable pair must be: the same shape the saving
// service refuses a contribution over, stated here so a declaration and the
// contribution that answers it cannot drift apart.
var savingSlug = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidSavingSlug reports whether s is one lower-case slug of at most 40
// characters. The empty string is invalid: a pair is named by an author who meant
// to name one. The saving service asks this of the pair a source contributes to
// it, so the declaration in a catalogue and the contribution behind it are held
// to one grammar rather than to two that happen to agree today.
func ValidSavingSlug(s string) bool { return savingSlug.MatchString(s) }

// ValidSavingWord reports whether word can be printed as one declared saving
// word: at most 256 UTF-8 bytes of valid UTF-8, no control character, and not
// nothing but spaces. The empty word is valid, because an empty word selects the
// fallback the resolver answers with rather than a word the author got wrong; the
// bound says only what a printed word may never be. The saving service applies it
// to the words a source contributes, so a declaration the mount gate accepts is
// never a contribution the service refuses over the same word.
func ValidSavingWord(word string) bool {
	return word == "" || (len(word) <= 256 && utf8.ValidString(word) &&
		strings.TrimSpace(word) != "" && !strings.ContainsFunc(word, unicode.IsControl))
}

// SavingValid asks the shape questions of a declaration: a slug pair, the two
// fields it names, and four words a screen can print. It is the whole bound a
// mount gate can check without a request — what a kept copy means stays with the
// source that owns it — and it is the bound the service applies to the
// contribution that matches it, so one refusal serves both sides.
func SavingValid(h *SavingHints) error {
	if h == nil {
		return errors.New("entity: no saving declaration to check")
	}
	if !ValidSavingSlug(h.Module) || !ValidSavingSlug(h.Kind) {
		return errors.New("entity: savable " + h.Module + "/" + h.Kind + " is not a slug pair")
	}
	if strings.TrimSpace(h.KeyField) == "" || strings.TrimSpace(h.VersionField) == "" {
		return errors.New("entity: savable " + h.Module + "/" + h.Kind + " names no key or version field")
	}
	for _, word := range []string{h.Words.Save, h.Words.Saved, h.Words.Remove, h.Words.ListTitle} {
		if !ValidSavingWord(word) {
			return errors.New("entity: savable " + h.Module + "/" + h.Kind + " declares a word no screen can print")
		}
	}
	return nil
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
	Figure ChoiceFigure `json:"figure,omitempty"`
	// Label is what a header, a control and a term call this field. Absent, the
	// reading is display.FieldLabel, which is Humanize(Name).
	Label string `json:"label,omitempty"`
	// Help is the line under a control. Absent, it is Field.Doc.
	Help string `json:"help,omitempty"`
	// Section names an EntitySection.Key of the owning entry.
	Section string `json:"section,omitempty"`
	// Visibility is `shown` (absent), `detail` or `hidden`. A reading decision
	// and nothing else: the field stays in the schema, the PATCH and the JSON.
	//
	// Saying it at all is the narrower word about the same column as the older
	// `ui:"hide:list"` tag, and it wins both ways: `shown` puts a field the tag hid
	// back on the list, `detail` takes off the list one the tag left on.
	// See Field.OnList.
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
	// DoneValues is which of this field's enum values mean *finished* — the
	// subset a shell may celebrate, and nothing else. It is orthogonal to
	// EnumTones: which values are finished is the entity's fact, which colour
	// finished wears is a product's copy decision. It is declared on the entry's
	// `statusField` alone, because the state a shell celebrates is the one the
	// row's pill shows; the mount gate refuses it anywhere else. An absent list
	// means nothing is finished and nothing celebrates.
	DoneValues []string `json:"doneValues,omitempty"`
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

// OffList reports whether a declared visibility keeps the field off a list of
// rows. `hidden` is off both screens and so off this one; `detail` is the field a
// record answers and a row does not need. `shown` — and the silence that means it
// — is on it.
//
// It reads the declaration and nothing else. Field.OnList is the whole answer,
// because a second, older tag says the same thing about the same column.
func (h FieldHints) OffList() bool { return h.Visibility == "hidden" || h.Visibility == "detail" }

// OnList reports whether a list of rows carries this field. It is where the two
// ways to say the same thing about one column are resolved, and the resolution is
// the brief's: the explicit hint wins over `hide:list`.
//
// Which of the two is the author's later word is not a fact a schema can carry,
// so the rule is by kind rather than by date: `visibility` names one field's
// reading on every screen, `hide:list` names one column of one screen, and the
// narrower declaration is the one that reads as an override. Both directions are
// the same precedence — `shown` reclaims a column `hide:list` took away, `detail`
// gives up a column `hide:list` left in place — so no pair of declarations about
// one field is refused: a contract that refused the pair that agreed and honoured
// the pair that disagreed would refuse the wrong one.
func (f Field) OnList() bool {
	if f.Presentation.Visibility == "" {
		return !f.HideList
	}
	return !f.Presentation.OffList()
}
