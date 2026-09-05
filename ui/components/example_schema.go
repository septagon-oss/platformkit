package components

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type exampleField struct {
	name     string
	typ      reflect.Type
	index    []int
	omit     bool
	zero     bool
	optional bool // A nil anonymous pointer omits its promoted fields.
}

func exampleFields(typ reflect.Type) ([]exampleField, error) {
	var fields []exampleField
	seen := map[string]bool{}
	visiting := map[reflect.Type]bool{}
	var walk func(reflect.Type, []int, bool) error
	walk = func(t reflect.Type, prefix []int, optional bool) error {
		if visiting[t] {
			return fmt.Errorf("recursive embedded type %s is unsupported", t)
		}
		visiting[t] = true
		defer delete(visiting, t)
		for field := range t.Fields() {
			if !field.IsExported() || field.Tag.Get("delivery") == "internal" {
				continue
			}
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			index := append(slices.Clone(prefix), field.Index...)
			nested := field.Type
			if nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if field.Anonymous && nested == reflect.TypeFor[HTMXProps]() {
				continue
			}
			if field.Anonymous && name == "" && nested.Kind() == reflect.Struct {
				if err := walk(nested, index, optional || field.Type.Kind() == reflect.Pointer); err != nil {
					return err
				}
				continue
			}
			if name == "" {
				name = field.Name
			}
			if seen[name] {
				return fmt.Errorf("ambiguous JSON field %q", name)
			}
			seen[name] = true
			flags := strings.Split(options, ",")
			if slices.Contains(flags, "string") {
				return fmt.Errorf("JSON string coercion on %q is unsupported", name)
			}
			fields = append(fields, exampleField{name, field.Type, index, slices.Contains(flags, "omitempty"), slices.Contains(flags, "omitzero"), optional})
		}
		return nil
	}
	err := walk(typ, nil, false)
	return fields, err
}

func exampleFieldValue(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

type exampleSchemaBuilder struct {
	refs map[reflect.Type]string
	defs map[string]any
}

func exampleHasJSONCodec(typ reflect.Type) bool {
	for _, codec := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[json.Unmarshaler](), reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextUnmarshaler]()} {
		if typ.Implements(codec) || reflect.PointerTo(typ).Implements(codec) {
			return true
		}
	}
	return false
}

func exampleSchema(typ reflect.Type) (map[string]any, error) {
	builder := exampleSchemaBuilder{refs: map[reflect.Type]string{typ: "#"}, defs: map[string]any{}}
	schema, err := builder.build(typ, true)
	if err == nil && len(builder.defs) > 0 {
		schema["$defs"] = builder.defs
	}
	return schema, err
}

func (b exampleSchemaBuilder) build(typ reflect.Type, definition bool) (map[string]any, error) {
	if exampleHasJSONCodec(typ) {
		return nil, fmt.Errorf("custom JSON codec for %s is unsupported", typ)
	}
	if typ == reflect.TypeFor[json.Number]() {
		return map[string]any{"type": "number"}, nil
	}
	if !definition && typ.Name() != "" && (typ.Kind() == reflect.Struct || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map) {
		if ref, ok := b.refs[typ]; ok {
			return map[string]any{"$ref": ref}, nil
		}
		name := fmt.Sprintf("type%d", len(b.refs))
		ref := "#/$defs/" + name
		b.refs[typ] = ref
		value, err := b.build(typ, true)
		b.defs[name] = value
		return map[string]any{"$ref": ref}, err
	}
	schema := map[string]any{}
	switch typ.Kind() {
	case reflect.Pointer:
		item, err := b.build(typ.Elem(), false)
		return map[string]any{"anyOf": []any{item, map[string]any{"type": "null"}}}, err
	case reflect.Struct:
		fields, err := exampleFields(typ)
		if err != nil {
			return nil, err
		}
		properties := map[string]any{}
		var required []string
		for _, field := range fields {
			properties[field.name], err = b.build(field.typ, false)
			if err != nil {
				return nil, fmt.Errorf("property %q: %w", field.name, err)
			}
			if !field.omit && !field.zero && !field.optional {
				required = append(required, field.name)
			}
		}
		schema = map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		if typ.Kind() == reflect.Map && typ.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key %s is not a string", typ.Key())
		}
		item, err := b.build(typ.Elem(), false)
		if err != nil {
			return nil, err
		}
		if typ.Kind() == reflect.Map {
			schema = map[string]any{"type": []string{"object", "null"}, "additionalProperties": item}
		} else if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			schema = map[string]any{"type": []string{"string", "null"}, "contentEncoding": "base64"}
		} else {
			schema = map[string]any{"type": "array", "items": item}
			if typ.Kind() == reflect.Slice {
				schema["type"] = []string{"array", "null"}
			} else {
				schema["minItems"], schema["maxItems"] = typ.Len(), typ.Len()
			}
		}
	case reflect.String:
		schema["type"] = "string"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema["type"] = "integer"
		if typ.Kind() >= reflect.Uint {
			schema["minimum"] = 0
		}
	case reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Interface:
		if typ.NumMethod() > 0 {
			return nil, fmt.Errorf("non-data interface %s is unsupported", typ)
		}
	default:
		return nil, fmt.Errorf("Props type %s is unsupported", typ)
	}
	return schema, nil
}

func portableExampleValue(v reflect.Value, active map[exampleCopyKey]bool) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	if v.Type().Implements(exampleNodeType) || exampleHasJSONCodec(v.Type()) {
		return nil, fmt.Errorf("opaque Props value %s is unsupported", v.Type())
	}
	key := exampleValueKey(v)
	if key.ptr != 0 {
		if active[key] {
			return nil, fmt.Errorf("cyclic Props value %s is unsupported", v.Type())
		}
		active[key] = true
		defer delete(active, key)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		return portableExampleValue(v.Elem(), active)
	case reflect.Struct:
		fields, err := exampleFields(v.Type())
		if err != nil {
			return nil, err
		}
		object := map[string]any{}
		for _, field := range fields {
			value, err := v.FieldByIndexErr(field.index)
			if err != nil {
				continue
			}
			empty := value.IsZero()
			if value.Kind() == reflect.Struct || value.Kind() == reflect.Array {
				empty = false
			}
			if value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.String || value.Kind() == reflect.Array {
				empty = value.Len() == 0
			}
			if (field.omit && empty) || (field.zero && value.IsZero()) {
				continue
			}
			object[field.name], err = portableExampleValue(value, active)
			if err != nil {
				return nil, err
			}
		}
		return object, nil
	case reflect.Map:
		if v.IsNil() {
			return nil, nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key %s is not a string", v.Type().Key())
		}
		object := map[string]any{}
		for iter := v.MapRange(); iter.Next(); {
			value, err := portableExampleValue(iter.Value(), active)
			if err != nil {
				return nil, err
			}
			object[iter.Key().String()] = value
		}
		return object, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && (v.IsNil() || v.Type().Elem().Kind() == reflect.Uint8) {
			return v.Interface(), nil
		}
		array := make([]any, v.Len())
		for i := range v.Len() {
			value, err := portableExampleValue(v.Index(i), active)
			if err != nil {
				return nil, err
			}
			array[i] = value
		}
		return array, nil
	default:
		return v.Interface(), nil
	}
}
