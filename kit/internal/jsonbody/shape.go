package jsonbody

import (
	"encoding"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
)

// Shape describes JSON fields without retaining request values.
type Shape struct {
	Type    reflect.Type
	Integer bool
	Opaque  bool
	Elem    *Shape
	Fields  []integerField
}

// integerField retains the declaration's exact tags and encoding/json's selected name.
type integerField struct {
	Name  string
	Index []int
	Tag   reflect.StructTag
	Shape *Shape
}

// Describe follows JSON-visible fields, including embedding and named integer types.
// Explicit JSON/text codecs and byte containers retain their owner's representation.
func Describe(t reflect.Type) *Shape {
	return buildIntegerShape(t, map[reflect.Type]*Shape{})
}

func buildIntegerShape(t reflect.Type, seen map[reflect.Type]*Shape) *Shape {
	if t == nil {
		return nil
	}
	if s := seen[t]; s != nil {
		return s
	}
	s := &Shape{Type: t}
	seen[t] = s
	s.Opaque = ownCodec(t) || (t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8)
	if s.Opaque {
		return s
	}
	s.Integer = t.Kind() == reflect.Int64
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		s.Elem = buildIntegerShape(t.Elem(), seen)
	case reflect.Struct:
		for _, f := range jsonFields(t) {
			s.Fields = append(s.Fields, integerField{Name: f.Name, Index: f.Index, Tag: f.Tag,
				Shape: buildIntegerShape(f.Type, seen)})
		}
	}
	return s
}

func ownCodec(t reflect.Type) bool {
	for _, iface := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[json.Unmarshaler](),
		reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextUnmarshaler]()} {
		if t.Implements(iface) || (t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(iface)) {
			return true
		}
	}
	return false
}

func jsonFields(t reflect.Type) []reflect.StructField {
	type embedded struct {
		typ     reflect.Type
		index   []int
		parents []reflect.Type
	}
	pending := []embedded{{typ: t}}
	candidates := map[string][]reflect.StructField{}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		if slices.Contains(current.parents, current.typ) {
			continue
		}
		for i := range current.typ.NumField() {
			f := current.typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" || (!f.IsExported() && !f.Anonymous) {
				continue
			}
			f.Index = append(slices.Clone(current.index), i)
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if f.Anonymous && name == "" && inner.Kind() == reflect.Struct {
				pending = append(pending, embedded{inner, f.Index, append(slices.Clone(current.parents), current.typ)})
				continue
			}
			if !f.IsExported() {
				continue
			}
			if name != "" {
				f.Name = name
			}
			candidates[f.Name] = append(candidates[f.Name], f)
		}
	}
	var fields []reflect.StructField
	for _, group := range candidates {
		// Shallowest fields win; at the same depth an explicit JSON name wins.
		rank := func(f reflect.StructField) int {
			n := 2 * len(f.Index)
			if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" {
				n--
			}
			return n
		}
		slices.SortFunc(group, func(a, b reflect.StructField) int { return rank(a) - rank(b) })
		if len(group) == 1 || rank(group[0]) != rank(group[1]) {
			fields = append(fields, group[0])
		}
	}
	slices.SortFunc(fields, func(a, b reflect.StructField) int { return slices.Compare(a.Index, b.Index) })
	return fields
}

// Member matches exact names before the case-insensitive fallback used by encoding/json.
func (s *Shape) Member(name string) *integerField {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	for i := range s.Fields {
		if strings.EqualFold(s.Fields[i].Name, name) {
			return &s.Fields[i]
		}
	}
	return nil
}

func hasInteger(s *Shape, seen map[*Shape]bool) bool {
	if s == nil || s.Opaque || seen[s] {
		return false
	}
	seen[s] = true
	if s.Integer || hasInteger(s.Elem, seen) {
		return true
	}
	for _, f := range s.Fields {
		if hasInteger(f.Shape, seen) {
			return true
		}
	}
	return false
}

// HasInteger reports whether a declared JSON member uses the signed 64-bit codec.
func (s *Shape) HasInteger() bool { return hasInteger(s, map[*Shape]bool{}) }

func (s *Shape) base() *Shape {
	for s != nil && !s.Opaque && s.Type.Kind() == reflect.Pointer {
		s = s.Elem
	}
	return s
}
