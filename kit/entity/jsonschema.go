// jsonschema.go is the projection of the field vocabulary onto JSON Schema
// 2020-12 — the vocabulary everything outside this repository already speaks.
// OpenAPI documents, MCP tool definitions, form renderers and validators read
// that shape, and none of them has to learn what a FieldType is to use one.
//
// It is a projection and not a second owner. Field stays authoritative: the
// document is derived from the fields on every call, is never stored, and is
// never edited by hand, so there is nothing here to keep in step with the
// struct it came from. What it says is the shape and JSON type of a record the
// API takes — not who may send it, which transition is legal, or what an amount
// is denominated in. See docs/adr/0012.

package entity

import (
	"fmt"
	"slices"
	"strconv"
)

// schemaDraft is the dialect JSONSchema writes, named in every document it
// returns so a consumer applies the rules this document was written under and
// not the ones it happens to know.
const schemaDraft = "https://json-schema.org/draft/2020-12/schema"

// widgetKey is how the projection carries Field.Widget. It is an extension and
// not a keyword because "which control draws this" is PlatformKit's question
// rather than the dialect's: a validator ignores an unknown x- member and the
// renderer that knows the name reads it.
const widgetKey = "x-platformkit-widget"

// JSONSchema returns a JSON Schema 2020-12 object schema for a record made of
// fields — an entity, or the argument of a command, which is the same question
// asked of a struct that names no table.
//
// It is pure: no I/O, no globals, no reflection, and nothing but the standard
// library. The fields are the caller's and are not modified; the returned
// document is the caller's to keep. Output is deterministic — encoding/json
// sorts a map's keys, which is the only ordering this promises.
//
// The error reports a declaration that cannot be projected, which today means
// one thing: a `default:"…"` that does not parse as the type beside it. The
// alternative is to emit the tag's text where a number belongs, which would put
// a value in the document that the type next to it refuses.
func JSONSchema(fields []Field) (map[string]any, error) {
	props := make(map[string]any, len(fields))
	var required []string
	for _, f := range fields {
		p, err := property(f)
		if err != nil {
			return nil, err
		}
		props[f.Name] = p
		// A read-only field is the server's to set, so a request that had to
		// send it could not create the row at all: Required together with
		// ReadOnly means "the owner of this value is not the caller".
		if f.Required && !f.ReadOnly {
			required = append(required, f.Name)
		}
	}
	doc := map[string]any{
		"$schema": schemaDraft,
		"type":    "object",
		// A property nobody declared is a misspelling and not an extension:
		// the fields the caller derived are the whole record, and rest's PATCH
		// merge refuses an unknown name the same way.
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		doc["required"] = required
	}
	return doc, nil
}

// property is one field: its shape, and the few things the dialect says about
// how a value is chosen rather than what it is.
func property(f Field) (map[string]any, error) {
	var prop map[string]any
	if f.Type == TypeList {
		// Fields derives no list of lists — fieldType drops a slice whose
		// element is not a scalar — so items is always a scalar here.
		prop = map[string]any{"type": "array", "items": jsonOf(f.Elem)}
	} else {
		prop = jsonOf(f.Type)
	}
	if len(f.Enum) > 0 {
		prop["enum"] = slices.Clone(f.Enum)
	}
	if f.Doc != "" {
		prop["description"] = f.Doc
	}
	if f.ReadOnly {
		prop["readOnly"] = true
	}
	if f.Default != "" {
		v, err := jsonDefault(f)
		if err != nil {
			return nil, err
		}
		prop["default"] = v
	}
	switch {
	case f.Widget != "":
		prop[widgetKey] = f.Widget
	case f.Type == TypeText:
		// A text column is a paragraph and a varchar a line, which is the
		// whole difference a control cares about. The storage tag already
		// decided it, so the schema names the control that difference means.
		prop[widgetKey] = "textarea"
	}
	// HideList and Present are not here on purpose: one is about which screen a
	// column appears on and the other about how a value is read, and neither is
	// a fact about the record a caller sends.
	return prop, nil
}

// jsonOf is the JSON shape of one FieldType that is not a list — the same
// answer for a field and for what a list holds. A list is the caller's to say,
// because only the field knows what it holds.
func jsonOf(t FieldType) map[string]any {
	switch t {
	case TypeInt:
		return map[string]any{"type": "integer"}
	case TypeFloat:
		return map[string]any{"type": "number"}
	case TypeBool:
		return map[string]any{"type": "boolean"}
	case TypeTime:
		return map[string]any{"type": "string", "format": "date-time"}
	case TypeUUID:
		return map[string]any{"type": "string", "format": "uuid"}
	default:
		// TypeString and TypeText, and a name this version cannot see: JSON's
		// most permissive scalar is the least wrong reading of an unknown type.
		return map[string]any{"type": "string"}
	}
}

// jsonDefault converts a declared default to the JSON type the property just
// got. The tag is a string because a struct tag is a string; the document is
// not, and a `default` of the wrong JSON type contradicts the `type` printed
// beside it.
//
// The conversion is to the JSON type and no further: an instant or an
// identifier is a string in JSON, so its default is emitted as declared and
// `format` stays the annotation the dialect says it is. Asserting a format is
// the validator's work, and it is the same work it does for a supplied value.
func jsonDefault(f Field) (any, error) {
	switch f.Type {
	case TypeInt:
		n, err := strconv.ParseInt(f.Default, 10, 64)
		if err != nil {
			return nil, undefault(f)
		}
		return n, nil
	case TypeFloat:
		n, err := strconv.ParseFloat(f.Default, 64)
		if err != nil {
			return nil, undefault(f)
		}
		return n, nil
	case TypeBool:
		b, err := strconv.ParseBool(f.Default)
		if err != nil {
			return nil, undefault(f)
		}
		return b, nil
	case TypeList:
		// No separator is defined for a default list anywhere the foundation
		// reads, and inventing one here would make this document the only place
		// that spellings like "a,b" meant an array.
		return nil, fmt.Errorf("entity: field %q: a list cannot carry the default %q", f.Name, f.Default)
	default:
		return f.Default, nil
	}
}

func undefault(f Field) error {
	return fmt.Errorf("entity: field %q: default %q does not parse as a %s", f.Name, f.Default, f.Type)
}
