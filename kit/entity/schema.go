package entity

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Schema describes an entity for consumers that did not compile against it.
// An integration supplies its module, resource name and path.
type Schema struct {
	Module string  `json:"module"`
	Entity string  `json:"entity"`
	Path   string  `json:"path"`
	Fields []Field `json:"fields"`
}

// RevisionField is the json name of a row's own write counter: the number a
// stale-base refusal quotes, and the only thing on a row that counts its writes.
// A Spec that carries it moves it itself, in the same UPDATE as the write that
// moved anything else (kit/rest), and a caller may read it and may not name it.
const RevisionField = "revision"

// RevisionCondition is the name a generated edit form posts the row's write count back
// under: the number the form was drawn from, carried so the write can be refused against
// a row that has moved since the person read it.
//
// It is deliberately not RevisionField. A body's "revision" is a value to store, which
// every write door refuses — a number a caller can set is no use for refusing a stale
// write — while this is a condition on the write and writes nothing itself. Two names,
// because the two questions differ; one owner, because the door that draws the form and
// the door that reads it may not then disagree about what the form carried.
const RevisionCondition = "expectedRevision"

// ServerOwned reports whether a field belongs to the server at a write door.
//
// There are two kinds. Base's, which ReadOnly already names: the id, the tenant
// and the two timestamps. And the row's own write counter, which an entity writes
// beside them and which is nobody's else to set: a number a caller can type is not
// a number a stale write can be refused with, and the two facts — the document says
// readOnly, the kernel moves it — are one rule seen from either side.
//
// A door that took either from a body stored what was sent and lost the record it
// was meant to be keeping, so every write door refuses or discards them together,
// asked through this one question rather than a second list somebody maintains.
func ServerOwned(f Field) bool {
	return f.ReadOnly || (f.Name == RevisionField && f.Type == TypeInt)
}

// FieldType is the closed set of shapes a screen knows how to render and a
// query knows how to compare. A field of any other Go type is left out of the
// schema entirely, so it is neither rendered nor sortable nor filterable — it
// is still stored, and still in the JSON, because that is encoding/json's
// business and not this package's.
type FieldType string

const (
	TypeString FieldType = "string"
	TypeText   FieldType = "text"
	TypeInt    FieldType = "int"
	TypeFloat  FieldType = "float"
	TypeBool   FieldType = "bool"
	TypeTime   FieldType = "time"
	TypeUUID   FieldType = "uuid"
	// TypeList is a slice of one of the above, which Field.Elem names. A user's
	// roles is the case that made it necessary: without it the field was in no
	// schema, so it rendered nowhere, no filter could refuse it and Immutable
	// could not name it — a PATCH could not reach it either, but only because
	// the field did not exist, which is the right answer for the wrong reason.
	TypeList FieldType = "list"
	// TypeMap is a value keyed by a name rather than by position: the text of each
	// translatable field of one record, keyed by that field's own name, is the case
	// that made it necessary. It is never *derived* — a map column stays out of an
	// entity's schema, because a column of many values is neither one a screen
	// draws nor one an equality filter compares — and appears only where the owner
	// of a command's argument declares it, together with the keys it takes. Which
	// keys a map takes is a fact about the record the command acts on, and no
	// reading of `map[string]string` can ever learn that this entity has a title
	// and a body.
	TypeMap FieldType = "map"
)

// MapArg is one map-valued argument of a command, declared by whoever owns the
// command: the shape Go cannot state and the keys no reflection can discover.
//
// It is a declaration and not a type, because the two differ exactly here: the Go
// type says "text keyed by a string", and the screen needs "one box, per field,
// for the fields of *this* record". The argument it declares one for has to be a
// map keyed by a string; kit/rest refuses a declaration that names anything else,
// so a key list can never describe an argument that would ignore it.
type MapArg struct {
	// Elem is what each key's value is: TypeString, TypeText or TypeInt.
	Elem FieldType `json:"elem"`
	// Doc is the sentence a shell puts under the group of controls.
	Doc string `json:"doc,omitempty"`
	// Keys is what the map takes, in the order a form should ask for it.
	Keys []MapKey `json:"keys,omitempty"`
}

// MapKey is one key of a declared map argument, and the whole reason a map can
// appear on a screen: a form cannot draw a box for a type, and a command whose
// keys come from the record it acts on — a translation's per-field text — declares
// them at mount, in the record's own field names.
type MapKey struct {
	// Name is the key, spelled as the JSON body spells it.
	Name string `json:"name"`
	// Label is the word a shell puts beside this key's control. Empty means the
	// shell humanises Name, which is right for a key named after a field.
	Label string `json:"label,omitempty"`
	// Widget names the control, from the same vocabulary Field.Widget names.
	Widget string `json:"widget,omitempty"`
	// From names the one fact about the record this key is filled *for* the
	// person, rather than typed by them: "revision" is the field's current
	// revision, which a screen has read and a person could not. Empty means
	// nobody but a person fills this key in.
	From string `json:"from,omitempty"`
}

// The facts a screen can fill a map key with on a person's behalf.
const (
	// KeyFromRevision is the field's current revision as the screen read it, 0
	// for a field with nothing stored. A write that carries it is the optimistic
	// check; one that carries none of them is a write that overwrites blind.
	KeyFromRevision = "revision"
)

// Field is one column, as the API and a screen see it.
type Field struct {
	Choices *ChoiceSet `json:"choices,omitempty"`
	// Name is the JSON name, which is the only name a caller ever uses.
	Name string `json:"name"`
	// Column is the storage column used by the CRUD adapter. It comes from
	// the struct, never from a request; deriving it executes no SQL.
	Column string    `json:"-"`
	Type   FieldType `json:"type"`
	// Elem is what a TypeList holds, and empty for everything else.
	Elem FieldType `json:"elem,omitempty"`
	// Keys is the closed set of keys a TypeMap argument takes. Reflection derives
	// a map's shape and cannot derive its keys, so the owner declares them; a
	// TypeMap field with none is a map no shell can fill in.
	Keys []MapKey `json:"keys,omitempty"`
	// Widget overrides the control a screen would pick from Type: `ui:"widget:select"`.
	// It must be a name Widgets admits; a Spec whose entity carries any other
	// name refuses to mount, because a name no renderer knows used to draw a
	// plain text input in silence.
	Widget string `json:"widget,omitempty"`
	// MaxLength is the declared character ceiling a generated control shows.
	MaxLength int `json:"maxLength,omitempty"`
	// Present selects a read renderer from Presentations, independently of Widget.
	// kit/rest refuses unknown names at mount; entity.Schema also carries the
	// selection to native consumers through the workspace catalog.
	Present string `json:"present,omitempty"`
	// Enum is the closed set of values, from `enum:"open,done"`.
	Enum []string `json:"enum,omitempty"`
	// Required comes from `validate:"required"`.
	Required bool `json:"required,omitempty"`
	// ReadOnly marks the fields Base contributes: a caller may read them and
	// may not write them, so they are skipped by the PATCH merge.
	ReadOnly bool `json:"readOnly,omitempty"`
	// HideList keeps a field off the list screen, from `ui:"hide:list"`.
	HideList bool `json:"hideList,omitempty"`
	// Display marks the field a row is named by: `ui:"display"`. Not "is displayed" — every field is.
	// A schema had no way to say which of its fields is the row's name, and a screen that cannot ask
	// renders an id: ui/resource takes the heading of a record, the text of the row's only link and the
	// tab title from this field when the entity declares one, and from its own fallback order when it
	// does not. A schema names one row one field: kit/rest refuses a second mark at mount.
	Display bool `json:"display,omitempty"`
	// Default is the value the entity declares for a field a caller may leave
	// out, from `default:"open"` — the same tag huma reads, so the form and the
	// API document agree about what happens when nothing is sent. A form
	// preselects it, and a select that has one needs no "Choose a …" placeholder
	// because there is no unchosen state to name.
	Default string `json:"default,omitempty"`
	// Doc is what the field is for, from `doc:"Lifecycle state"` — again huma's
	// own tag, so the sentence in the OpenAPI document is the sentence under the
	// control. It is a description and not a label: the entities here write
	// "Short summary of the task", which reads under an input and not on it.
	Doc string `json:"doc,omitempty"`
	// Translatable comes from `i18n:"translatable"` and means one thing: a
	// tenant may hold this field's value in each language it is served in, and
	// a reader may ask for one of them. It is a fact about the field and not
	// about a table — the rows live in modules/translation, keyed by this
	// entity's own two names — and it is what kit/rest refuses a mount over when
	// the module that mounted the Spec wired no Translations port, because a
	// tag nobody can honour is a tag that silently does nothing.
	//
	// It reaches the native catalogue for free: ui/screens' Entry embeds this
	// Schema, so a phone can draw a completeness chip without a second
	// declaration of which fields are translatable.
	Translatable bool `json:"translatable,omitempty"`

	// Presentation is how a read names, groups, colours and shows this field,
	// from the `ui:` directives of §2.3 and the `enumLabels`/`enumTones` tags.
	// It is a value and not a pointer because the zero FieldHints is exactly "no
	// body said anything about this field" and omitzero prints no key for it; a
	// pointer would give the same nothing two shapes, one of which a shell would
	// have to nil-check for every field of every entity.
	//
	// It sits beside Widget, Present and Display rather than replacing any of
	// them: what to type into, how to read and how much to show are three
	// questions with three answers.
	Presentation FieldHints `json:"presentation,omitzero"`

	// Index locates the field in the struct. It is exported for one caller,
	// kit/rest's PATCH merge, which decodes a body into the field this names;
	// json:"-" because a screen has no use for it and a caller none at all.
	Index []int `json:"-"`
}

// Widgets lists the accepted ui:widget names rendered by ui/forms.
// kit/rest refuses names outside this vocabulary. datetime and checkbox also
// allow string-backed fields to select the controls their typed equivalents use.
// tiles, chips, figure and swatch are the four appearances a finite choice draws:
// each is a set of inputs a person operates, so the appearance is a widget and not
// a read name, and a choice names it once, in `ui:"widget:…"`.
var Widgets = []string{
	"checkbox", "color", "date", "datetime", "email", "entity-picker", "file",
	"hidden", "month", "number", "password", "search", "select", "tel", "text",
	"textarea", "richtext", "time", "url", "week", "tiles", "chips", "figure", "swatch",
}

// Presentations lists the ui:present names rendered by ui/resource.
// An empty name derives presentation from the field type. kit/rest refuses
// unknown names and incompatible picture shapes at mount, and refuses a control
// name here: a finite choice declares its appearance as a widget and keeps this
// read axis empty, which ValidateChoices checks.
var Presentations = []string{"person", "image", "images", "avatar"}

// PicturesAre reports whether the named presentation reads a file id, which is
// the one fact about the name that a mount gate can check without importing what
// draws it: `image`, `images` and `avatar` hold a `files` row's id and nothing
// else, so they refuse the column shapes that could not hold one and the hints
// that would read the same column two ways.
func PicturesAre(present string) bool {
	return present == "image" || present == "images" || present == "avatar"
}

// ValidPresentation reports whether a screen renders the named presentation. The
// empty string is valid: it means "derive from Type and Enum", which is most
// fields and every field written before this axis existed.
func ValidPresentation(present string) bool {
	if present == "" {
		return true
	}
	for _, p := range Presentations {
		if p == present {
			return true
		}
	}
	return false
}

// ValidWidget reports whether a screen can draw the named widget. The empty
// string is valid: it means "pick from Type", which is most fields.
func ValidWidget(widget string) bool {
	if widget == "" {
		return true
	}
	for _, w := range Widgets {
		if w == widget {
			return true
		}
	}
	return false
}

// schemas caches one derivation per entity type. Reflection over a struct is
// cheap but it is not free, and a list request would otherwise pay for it
// twice.
var schemas sync.Map // reflect.Type -> []Field

// Fields derives the schema of a pointer entity type. Every call returns its
// own fields, enum values and index paths; callers may customize that metadata
// without changing another consumer's schema.
func Fields[T Entity]() []Field {
	return FieldsOf(reflect.TypeFor[T]().Elem())
}

// FieldsOf derives metadata for a struct or a pointer to one, including plain
// command inputs that do not embed Base. Nil and non-struct types have no fields.
// The returned fields, enum values and index paths belong to the caller.
func FieldsOf(t reflect.Type) []Field {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	cached, ok := schemas.Load(t)
	if !ok {
		cached, _ = schemas.LoadOrStore(t, derive(t))
	}
	return copyFields(cached.([]Field))
}

func copyFields(fields []Field) []Field {
	out := slices.Clone(fields)
	for i := range out {
		if out[i].Choices != nil {
			out[i].Choices = out[i].Choices.Clone()
		}
		out[i].Enum = slices.Clone(out[i].Enum)
		out[i].Index = slices.Clone(out[i].Index)
		// The two hint maps are the new way to break the promise Fields makes
		// above — a caller may customise this metadata — so they are cloned with
		// the slices, or one consumer's label becomes the next caller's schema.
		out[i].Presentation.EnumLabels = maps.Clone(out[i].Presentation.EnumLabels)
		out[i].Presentation.EnumTones = maps.Clone(out[i].Presentation.EnumTones)
		if m := out[i].Presentation.Money; m != nil {
			money := *m
			out[i].Presentation.Money = &money
		}
		if ref := out[i].Presentation.Reference; ref != nil {
			reference := *ref
			out[i].Presentation.Reference = &reference
		}
		// Keys is the map argument's own declaration: a caller may customise
		// this metadata, so the slice of keys travels as a copy of its own.
		out[i].Keys = slices.Clone(out[i].Keys)
	}
	return out
}

// FieldNamed finds a field by its JSON name in the caller's metadata.
func FieldNamed(fields []Field, name string) (Field, bool) {
	if i := slices.IndexFunc(fields, func(f Field) bool { return f.Name == name }); i >= 0 {
		return fields[i], true
	}
	return Field{}, false
}

var (
	baseType = reflect.TypeFor[Base]()
	uuidType = reflect.TypeFor[uuid.UUID]()
	timeType = reflect.TypeFor[time.Time]()
)

// derive reads a struct into fields, following embedded structs so that Base's
// own columns appear, marked read-only.
func derive(t reflect.Type) []Field {
	var out []Field
	for _, sf := range reflect.VisibleFields(t) {
		f, ok := deriveField(t, sf)
		if !ok {
			continue
		}
		readUI(&f, sf.Tag.Get("ui"))
		readEnumPresentation(&f, sf.Tag)
		out = append(out, f)
	}
	return out
}

// declaredBy is the struct a promoted field was declared in, which is how a
// field of Base is told from a field of the entity.
func declaredBy(t reflect.Type, sf reflect.StructField) reflect.Type {
	if len(sf.Index) == 1 {
		return t
	}
	return t.FieldByIndex(sf.Index[:len(sf.Index)-1]).Type
}

// jsonName is the name the API speaks. A field tagged json:"-" is not part of
// the entity as far as anything outside the kernel is concerned.
func jsonName(sf reflect.StructField) (string, bool) {
	tag, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	switch tag {
	case "-":
		return "", false
	case "":
		return sf.Name, true
	default:
		return tag, true
	}
}

// fieldType maps a Go type to the closed set, reporting false for a type no
// screen and no filter can handle. A slice of one of the scalars is TypeList
// and the second return is what it holds.
func fieldType(sf reflect.StructField) (kind, elem FieldType, ok bool) {
	t := sf.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// A slice is a list of whatever it holds, and []byte is not one of those: a
	// blob is a single value, and rendering it as a list of small numbers is
	// worse than leaving it out. uuid.UUID is an array and not a slice, so it
	// never reaches here.
	if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8 {
		elem, ok = scalar(t.Elem(), sf.Tag)
		return TypeList, elem, ok
	}
	kind, ok = scalar(t, sf.Tag)
	return kind, "", ok
}

// scalar is the shape of a single value. The tag is a parameter because a text
// column is a paragraph and a varchar is a line, which is the whole difference
// a form cares about, so the storage decision that is already in the gorm tag
// is the one that decides the widget.
func scalar(t reflect.Type, tag reflect.StructTag) (FieldType, bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t {
	case reflect.TypeFor[Picture]():
		return TypePicture, true
	case uuidType:
		return TypeUUID, true
	case timeType:
		return TypeTime, true
	}
	switch t.Kind() {
	case reflect.Bool:
		return TypeBool, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return TypeInt, true
	case reflect.Float32, reflect.Float64:
		return TypeFloat, true
	case reflect.String:
		if has(tag.Get("gorm"), "type:text") {
			return TypeText, true
		}
		return TypeString, true
	default:
		return "", false
	}
}

// column is the database column: the gorm tag's own name when it names one, and
// otherwise the snake_case GORM would have derived.
func column(sf reflect.StructField) string {
	for part := range strings.SplitSeq(sf.Tag.Get("gorm"), ";") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(part), "column:"); ok {
			return name
		}
	}
	return snake(sf.Name)
}

func has(tag, want string) bool {
	for part := range strings.SplitSeq(tag, ";") {
		for item := range strings.SplitSeq(part, ",") {
			if strings.TrimSpace(item) == want {
				return true
			}
		}
	}
	return false
}

// snake is GORM's default naming: a word boundary is a lower-to-upper change,
// or the last capital of a run of them. "TenantID" is tenant_id, "DueAt" is
// due_at, "ID" is id.
func snake(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if !unicode.IsUpper(r) {
			b.WriteRune(r)
			continue
		}
		endsRun := i+1 < len(rs) && !unicode.IsUpper(rs[i+1])
		if i > 0 && (!unicode.IsUpper(rs[i-1]) || endsRun) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
