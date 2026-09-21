package entity

import (
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
)

// Field is one column, as the API and a screen see it.
type Field struct {
	// Name is the JSON name, which is the only name a caller ever uses.
	Name string `json:"name"`
	// Column is the storage column used by the CRUD adapter. It comes from
	// the struct, never from a request; deriving it executes no SQL.
	Column string    `json:"-"`
	Type   FieldType `json:"type"`
	// Elem is what a TypeList holds, and empty for everything else.
	Elem FieldType `json:"elem,omitempty"`
	// Widget overrides the control a screen would pick from Type: `ui:"widget:select"`.
	// It must be a name Widgets admits; a Spec whose entity carries any other
	// name refuses to mount, because a name no renderer knows used to draw a
	// plain text input in silence.
	Widget string `json:"widget,omitempty"`
	// Present is how a *read* renders the value: `ui:"present:person"`. It must be
	// a name Presentations admits, and a Spec whose entity names anything else
	// refuses to mount for the same reason Widget does — a name no renderer knows
	// would draw the raw value in silence and the declaration would mean nothing.
	//
	// It is a separate axis from Widget rather than another widget name, and the
	// reason is in Widgets' own comment: the control a widget names is drawn by
	// ui/forms. `select` is a thing a person types into; `person` is a way a value
	// is read, and there is no control to type a person's *name* into that a
	// read-only cell needs. Forcing every read form through the form vocabulary
	// would buy exactly one fake control per read form. Where a field genuinely is
	// both, name both: `ui:"widget:entity-picker;present:person"`.
	//
	// It reaches the native consumer without further work: ui/screens' Entry
	// embeds entity.Schema, so /api/v1/app/resources carries this field, and the
	// native renderer decides what a person looks like there.
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

	// Index locates the field in the struct. It is exported for one caller,
	// kit/rest's PATCH merge, which decodes a body into the field this names;
	// json:"-" because a screen has no use for it and a caller none at all.
	Index []int `json:"-"`
}

// Widgets is every name `ui:"widget:…"` may carry. It is the vocabulary, not a
// suggestion: the control each name draws lives in ui/forms, which cannot be
// imported from here, so what binds the two is a test there that names a
// rendered control for every entry here, and the mount-time refusal in
// kit/rest's Spec.check. A widget absent from this list is a compile-adjacent
// mistake rather than a runtime surprise: kit/entity owns the names because
// ui/forms owns the renderers, and a kernel below the presentation layer has no
// way to ask what a control is.
//
// Two entries are aliases of what a Go type already picks — `datetime` for a
// time.Time and `checkbox` for a bool — kept because fifteen fields of the
// foundation's own entities, and of the catalog and a client, name them. They
// now mean the control instead of being ignored next to it, which is why a
// string holding a timestamp or a "true" draws the same control its
// time.Time and bool neighbours do.
var Widgets = []string{
	"checkbox", "color", "date", "datetime", "email", "entity-picker", "file",
	"hidden", "month", "number", "password", "search", "select", "tel", "text",
	"textarea", "time", "url", "week",
}

// Presentations is every name `ui:"present:…"` may carry, and it is the read-axis
// twin of Widgets. The same argument binds it: the composition each name renders
// lives in ui/resource, which a kernel package below the presentation layer cannot
// import, so what ties the two together is a test there that names a rendered
// composition for every entry here, plus the mount-time refusal in kit/rest.
//
// One entry, on purpose. ADR 0012 requires an adopted consumer's benefit, and a
// vocabulary invented with no second caller in sight is how you get an alias of a
// Go type you have to keep explaining. `text` is deliberately absent: the empty
// string already means "derive it from the type", and an enum value, a timestamp
// and a boolean each have a reading that needs no name.
var Presentations = []string{"person"}

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
		out[i].Enum = slices.Clone(out[i].Enum)
		out[i].Index = slices.Clone(out[i].Index)
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
		if sf.Anonymous || !sf.IsExported() {
			continue
		}
		name, ok := jsonName(sf)
		if !ok {
			continue
		}
		kind, elem, ok := fieldType(sf)
		if !ok {
			continue
		}
		f := Field{
			Name:     name,
			Column:   column(sf),
			Type:     kind,
			Elem:     elem,
			Required: has(sf.Tag.Get("validate"), "required"),
			ReadOnly: declaredBy(t, sf) == baseType,
			Default:  sf.Tag.Get("default"),
			Doc:      sf.Tag.Get("doc"),
			Index:    sf.Index,
		}
		if enum := sf.Tag.Get("enum"); enum != "" {
			f.Enum = strings.Split(enum, ",")
		}
		// Either separator: a struct tag reads as one string, and the entities
		// in this repository were written with both. `ui:"widget:textarea;hide:list"`
		// used to parse as one directive whose value was "textarea;hide:list",
		// so the widget matched nothing and the field appeared on every list
		// screen it had asked to be kept off. There is no third spelling.
		for _, part := range strings.FieldsFunc(sf.Tag.Get("ui"), func(r rune) bool { return r == ',' || r == ';' }) {
			switch key, value, _ := strings.Cut(part, ":"); key {
			case "widget":
				f.Widget = value
			case "present":
				f.Present = value
			case "hide":
				f.HideList = f.HideList || value == "list"
			}
		}
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
