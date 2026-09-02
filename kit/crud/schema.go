package crud

import (
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Schema is what an entity looks like to something that did not compile against
// it: the generated screens of stage E4, and the sort and filter checks here.
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
)

// Field is one column, as the API and a screen see it.
type Field struct {
	// Name is the JSON name, which is the only name a caller ever uses.
	Name string `json:"name"`
	// Column is the database column, and the only string this package ever
	// interpolates into SQL. It comes from the struct, never from a request.
	Column string    `json:"-"`
	Type   FieldType `json:"type"`
	// Widget overrides the control a screen would pick from Type: `ui:"widget:select"`.
	Widget string `json:"widget,omitempty"`
	// Enum is the closed set of values, from `enum:"open,done"`.
	Enum []string `json:"enum,omitempty"`
	// Required comes from `validate:"required"`.
	Required bool `json:"required,omitempty"`
	// ReadOnly marks the fields Base contributes: a caller may read them and
	// may not write them, so they are skipped by the PATCH merge.
	ReadOnly bool `json:"readOnly,omitempty"`
	// HideList keeps a field off the list screen, from `ui:"hide:list"`.
	HideList bool `json:"hideList,omitempty"`

	// Index locates the field in the struct. It is exported for one caller,
	// kit/rest's PATCH merge, which decodes a body into the field this names;
	// json:"-" because a screen has no use for it and a caller none at all.
	Index []int `json:"-"`
}

// schemas caches one derivation per entity type. Reflection over a struct is
// cheap but it is not free, and a list request would otherwise pay for it
// twice.
var schemas sync.Map // reflect.Type -> []Field

// Fields derives the schema of T once and remembers it. A Spec's Schema is
// these fields plus the names the Spec gives the resource.
func Fields[T Entity]() []Field {
	t := reflect.TypeOf(blank[T]()).Elem()
	if cached, ok := schemas.Load(t); ok {
		return cached.([]Field)
	}
	fields := derive(t)
	schemas.Store(t, fields)
	return fields
}

var (
	baseType = reflect.TypeOf(Base{})
	uuidType = reflect.TypeOf(uuid.UUID{})
	timeType = reflect.TypeOf(time.Time{})
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
		kind, ok := fieldType(sf)
		if !ok {
			continue
		}
		f := Field{
			Name:     name,
			Column:   column(sf),
			Type:     kind,
			Required: has(sf.Tag.Get("validate"), "required"),
			ReadOnly: declaredBy(t, sf) == baseType,
			Index:    sf.Index,
		}
		if enum := sf.Tag.Get("enum"); enum != "" {
			f.Enum = strings.Split(enum, ",")
		}
		for _, part := range strings.Split(sf.Tag.Get("ui"), ",") {
			switch key, value, _ := strings.Cut(part, ":"); key {
			case "widget":
				f.Widget = value
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
// screen and no filter can handle.
func fieldType(sf reflect.StructField) (FieldType, bool) {
	t := sf.Type
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
		// A text column is a paragraph and a varchar is a line, which is the
		// whole difference a form cares about, so the storage decision that is
		// already in the gorm tag is the one that decides the widget.
		if has(sf.Tag.Get("gorm"), "type:text") {
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
	for _, part := range strings.Split(sf.Tag.Get("gorm"), ";") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(part), "column:"); ok {
			return name
		}
	}
	return snake(sf.Name)
}

func has(tag, want string) bool {
	for _, part := range strings.Split(tag, ";") {
		for _, item := range strings.Split(part, ",") {
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
