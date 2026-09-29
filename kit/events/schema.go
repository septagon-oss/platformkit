// schema.go projects an event's payload type into JSON Schema 2020-12 and
// checks a payload against that projection.
//
// The schema is derived, never written. A checked-in document beside a Go type
// is a second source of truth, and the two drift in the direction that is not
// caught: the Go type moves, the document does not, and the outbox then refuses
// the payload the program actually means to publish. So the projection reads
// reflect.Type and reads it the way encoding/json will: same json tags, same
// omitempty, same embedded-flattening, same unexported-field rule. What a
// publisher can produce and what a validator accepts are then one description.
//
// The subset is deliberate and small: the six JSON types, formats for the three
// shapes this program puts in a payload (uuid, date-time, and the base64 a byte
// string marshals to), no enum inference, and additionalProperties stays open. An
// open object is not laziness — a payload that gained a field is a payload whose
// subscriber reads the field it came for, while a payload whose email is a
// number or that lost its userId is a subscriber that guessed. Refusing the
// second and admitting the first is the whole job here.
package events

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Schema is the kernel's JSON Schema subset: enough to describe a payload
// honestly, little enough that a reader can hold the checker in their head.
type Schema struct {
	// Type is "", "object", "array", "string", "number", "integer" or
	// "boolean". "" is the any type: no constraint at all.
	Type string
	// Format is the JSON Schema format keyword, used for the three shapes this
	// program puts in a payload: "uuid", "date-time" and "byte". Every format
	// emitted by jsonSchema is enforced by check: a document that promises a
	// form the checker wave through is a promise nobody keeps.
	Format string
	// Properties is an object's fields, in declaration order.
	Properties []Property
	// Items is an array's element schema.
	Items *Schema
	// Values is a map's value schema: what every key's value must satisfy.
	// It is emitted as additionalProperties, which is how JSON Schema spells
	// "the keys are open, the values are not".
	Values *Schema
}

// Property is one field of an object schema, with whether it must be present.
type Property struct {
	Name string
	// Required is the mirror of the field's own `json:"…,omitempty"`: a field
	// that can be absent from the JSON is not required, and one that cannot is.
	Required bool
	Schema   *Schema
}

var (
	timeType       = reflect.TypeFor[time.Time]()
	uuidType       = reflect.TypeFor[uuid.UUID]()
	byteType       = reflect.TypeFor[byte]()
	byteSliceType  = reflect.TypeFor[[]byte]()
	rawMessageType = reflect.TypeFor[json.RawMessage]()
)

// schemas caches one projection per type. A struct's shape does not change
// under us, and every Publish of a declared event would otherwise re-walk it.
var schemas sync.Map // reflect.Type -> *Schema

// SchemaOf projects t, the Go type of an event payload. Nil means "no
// constraint": for a type the projection cannot describe — an interface, a map
// with non-string keys, a type that marshals itself to something unknowable —
// the honest answer is that the kernel does not know, and an honest unknown is
// checked by nobody rather than wrongly by everybody.
func SchemaOf(t reflect.Type) *Schema {
	t = deref(t)
	if t == nil {
		return nil
	}
	if cached, ok := schemas.Load(t); ok {
		return cached.(*Schema)
	}
	s := project(t)
	stored, _ := schemas.LoadOrStore(t, s)
	return stored.(*Schema)
}

func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func project(t reflect.Type) *Schema {
	t = deref(t)
	switch t {
	case nil:
		return nil
	case timeType:
		return &Schema{Type: "string", Format: "date-time"}
	case uuidType:
		return &Schema{Type: "string", Format: "uuid"}
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		// encoding/json has two sayings for a byte slice: []byte becomes a
		// base64 string, and json.RawMessage becomes the JSON it holds. A
		// projection that called either "an array of integers" would refuse
		// every payload that carried one, which is the drift this file exists
		// to avoid.
		if t == rawMessageType {
			return nil // already-marshalled JSON: the shape is whatever it holds
		}
		if t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string", Format: "byte"} // []byte, or a named byte slice like auth.Digest
		}
		return &Schema{Type: "array", Items: SchemaOf(t.Elem())}
	case reflect.Map:
		s := &Schema{Type: "object", Values: SchemaOf(t.Elem())}
		if t.Key().Kind() != reflect.String {
			// Keys that are not strings need encoding/json's stringified-key
			// rules, which this checker does not claim to implement.
			return nil
		}
		return s
	case reflect.Struct:
		if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) {
			// A type that writes its own JSON is only described by the JSON it
			// writes, and reflection cannot see that.
			return nil
		}
		s := &Schema{Type: "object"}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, opts := parseTag(f.Tag.Get("json"))
			if name == "-" || !f.IsExported() {
				continue
			}
			if f.Anonymous && name == "" {
				// embedding: encoding/json flattens it, so this must too.
				if inner := project(deref(f.Type)); inner != nil {
					s.Properties = append(s.Properties, inner.Properties...)
				}
				continue
			}
			if name == "" {
				name = f.Name
			}
			// A pointer field can be absent and stay absent; an omitempty one
			// can too. Neither is required.
			req := !slices.Contains(opts, "omitempty") && deref(f.Type) == f.Type
			s.Properties = append(s.Properties, Property{Name: name, Required: req, Schema: SchemaOf(f.Type)})
		}
		return s
	}
	// Interface, func, chan, complex: nothing honest to say.
	return nil
}

var marshalerType = reflect.TypeFor[json.Marshaler]()

func parseTag(tag string) (name string, opts []string) {
	name, rest, _ := strings.Cut(tag, ",")
	if rest != "" {
		opts = strings.Split(rest, ",")
	}
	return name, opts
}

// jsonSchema is the emission form: a JSON Schema 2020-12 document, which is
// what an AsyncAPI channel's message body and any external validator wants.
func (s *Schema) jsonSchema() any {
	if s == nil {
		return true // JSON Schema's "anything"
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	switch {
	case s.Type == "object" && len(s.Properties) > 0:
		props, required := map[string]any{}, []string{}
		for _, p := range s.Properties {
			props[p.Name] = p.Schema.jsonSchema()
			if p.Required {
				required = append(required, p.Name)
			}
		}
		out["properties"] = props
		if len(required) > 0 {
			out["required"] = required
		}
		if s.Values != nil {
			out["additionalProperties"] = s.Values.jsonSchema()
		}
	case s.Type == "object":
		// A map, or an object nothing constrained: the keys stay open.
		if s.Values != nil {
			out["additionalProperties"] = s.Values.jsonSchema()
		}
	case s.Type == "array":
		out["items"] = s.Items.jsonSchema()
	}
	return out
}

// JSONValue is the projection as a JSON value, with no $schema member. An
// enclosing document — an AsyncAPI file, a JSON Schema property — carries its
// own dialect, and a nested member claiming a second one is a contradiction a
// reader has to resolve by hand. Read this where the schema is a member of
// somebody else's document, and MarshalJSON where it is the whole one.
func (s *Schema) JSONValue() any { return s.jsonSchema() }

// MarshalJSON writes the schema as its own JSON Schema document.
func (s *Schema) MarshalJSON() ([]byte, error) {
	doc := s.jsonSchema()
	if m, ok := doc.(map[string]any); ok {
		m["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	}
	return json.Marshal(doc)
}

// Validate checks one payload against the schema. The error names the path and
// what was wanted, because it surfaces inside a business transaction and the
// person who reads it is the one who wrote the publisher.
func (s *Schema) Validate(body []byte) error {
	if s == nil {
		return nil // no projection means no constraint
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("the payload is not a JSON value: %w", err)
	}
	return s.check("", value)
}

func (s *Schema) check(path string, v any) error {
	if s == nil {
		// The same honest unknown Validate answers and jsonSchema emits as JSON
		// Schema's `true`. Recursing through a member the projection could not
		// describe — a json.RawMessage, an `any`, a []any, a map with non-string
		// keys, a type that marshals itself — is how an honest unknown turns into
		// a nil dereference inside the publisher's own transaction.
		return nil
	}
	at := func(field string) string {
		if path == "" {
			return "$." + field
		}
		return path + "." + field
	}
	if v == nil {
		// A null that reached here arrived as a declared, present field whose
		// value was null, or as the whole payload. Either way the schema says
		// what the member is, and null is not it.
		return fmt.Errorf("%s is null, want %s", orRoot(path), s.describe())
	}
	switch s.Type {
	case "":
		return nil
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is %s, want an object", orRoot(path), jsonKind(v))
		}
		for _, p := range s.Properties {
			got, present := obj[p.Name]
			if !present {
				if p.Required {
					return fmt.Errorf("%s is missing, and %s must be there", at(p.Name), p.Name)
				}
				continue
			}
			if got == nil {
				continue // an explicit null in an optional field is its absent form
			}
			if err := p.Schema.check(at(p.Name), got); err != nil {
				return err
			}
		}
		if s.Values != nil {
			for k, got := range obj {
				if err := s.Values.check(at(k), got); err != nil {
					return err
				}
			}
		}
		return nil
	case "array":
		items, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s is %s, want an array", orRoot(path), jsonKind(v))
		}
		for i, item := range items {
			if err := s.Items.check(fmt.Sprintf("%s[%d]", orRoot(path), i), item); err != nil {
				return err
			}
		}
		return nil
	case "string":
		text, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s is %s, want a string", orRoot(path), jsonKind(v))
		}
		switch s.Format {
		case "uuid":
			if _, err := uuid.Parse(text); err != nil {
				return fmt.Errorf("%s %q is not a UUID", orRoot(path), text)
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return fmt.Errorf("%s %q is not an RFC 3339 timestamp", orRoot(path), text)
			}
		case "byte":
			// encoding/json writes a []byte as padded standard base64, which is
			// what the document promises; anything else in that member arrived
			// by another route and is not the bytes the type declared.
			if _, err := base64.StdEncoding.DecodeString(text); err != nil {
				return fmt.Errorf("%s %q is not base64, which is how a byte string marshals", orRoot(path), text)
			}
		}
		return nil
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s is %s, want a boolean", orRoot(path), jsonKind(v))
		}
		return nil
	case "integer", "number":
		num, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("%s is %s, want %s", orRoot(path), jsonKind(v), s.Type)
		}
		if s.Type == "integer" {
			if _, err := num.Int64(); err != nil {
				return fmt.Errorf("%s %s is not an integer", orRoot(path), num)
			}
		}
		return nil
	}
	return nil
}

func (s *Schema) describe() string {
	if s.Format == "byte" {
		return "a base64 string"
	}
	if s.Format != "" {
		return "a " + s.Format
	}
	if s.Type == "" {
		return "any JSON value"
	}
	return "a " + s.Type
}

func orRoot(path string) string {
	if path == "" {
		return "$"
	}
	return path
}

func jsonKind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number:
		return "a number"
	}
	return "JSON " + fmt.Sprintf("%T", v)
}
