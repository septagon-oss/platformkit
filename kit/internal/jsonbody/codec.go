package jsonbody

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// Decode binds one JSON value exactly, retaining untyped numbers as json.Number.
// Scalar null keeps encoding/json's no-op; explicit codecs own their representation.
func Decode(data []byte, target any) error { return bind(data, target, reflect.Value{}) }

// Bind carries map/form values into their declared types, refusing floats whose
// precision cannot establish an exact int64. Caller-owned values are not changed.
func Bind(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bind(data, target, reflect.ValueOf(value))
}

func bind(data []byte, target any, value reflect.Value) error {
	typ := reflect.TypeOf(target)
	if typ == nil || typ.Kind() != reflect.Pointer || reflect.ValueOf(target).IsNil() {
		return &json.InvalidUnmarshalError{Type: typ}
	}
	data, err := Rewrite(data, Describe(typ.Elem()), value, false, true)
	if err != nil {
		return err
	}
	return Tokens(data, target)
}

// Tokens decodes one complete JSON value with exact untyped number tokens.
func Tokens(data []byte, target any) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON: expected one complete value")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(target)
}

// Encode writes decimal-string int64 values without changing the input.
func Encode(w io.Writer, value any) error {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return err
	}
	data, err := Rewrite(b.Bytes(), Describe(reflect.TypeOf(value)), reflect.ValueOf(value), true, true)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// Rewrite changes declared int64 tokens only; object order and duplicate members
// remain intact. legacyNull preserves scalar-null PATCH tolerance.
func Rewrite(data []byte, s *Shape, value reflect.Value, output, legacyNull bool) ([]byte, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON: expected one complete value")
	}
	return rewrite(data, s, value, output, legacyNull, "", "body")
}

func rewrite(data []byte, s *Shape, value reflect.Value, output, legacyNull bool, tag reflect.StructTag, path string) ([]byte, error) {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		value = value.Elem()
	}
	if s != nil && s.Type.Kind() == reflect.Interface && value.IsValid() && output {
		s = Describe(value.Type())
	}
	nullable := s == nil || s.Type.Kind() == reflect.Pointer
	s = s.base()
	if s == nil || s.Opaque {
		return data, nil
	}
	if s.Integer {
		return rewriteScalar(data, value, output, legacyNull, nullable, tag, path)
	}
	if s.Type.Kind() != reflect.Struct && s.Elem == nil {
		return data, nil
	}
	d := json.NewDecoder(bytes.NewReader(data))
	opening, _ := d.Token()
	if opening != json.Delim('{') && opening != json.Delim('[') {
		return data, nil
	}
	var out bytes.Buffer
	last, index := int64(0), 0
	for d.More() {
		name := strconv.Itoa(index)
		if opening == json.Delim('{') {
			token, _ := d.Token()
			name, _ = token.(string)
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		child, childValue, childTag := childOf(s, value, name, index)
		changed, err := rewrite(raw, child, childValue, output, legacyNull, childTag, path+"."+name)
		if err != nil {
			return nil, err
		}
		end := d.InputOffset()
		out.Write(data[last : end-int64(len(raw))])
		out.Write(changed)
		last, index = end, index+1
	}
	out.Write(data[last:])
	return out.Bytes(), nil
}

func childOf(s *Shape, value reflect.Value, name string, index int) (*Shape, reflect.Value, reflect.StructTag) {
	shape, tag := s.Elem, reflect.StructTag("")
	var child reflect.Value
	if s.Type.Kind() == reflect.Struct {
		if f := s.Member(name); f != nil {
			shape, tag = f.Shape, f.Tag
			if value.Kind() == reflect.Struct {
				child, _ = value.FieldByIndexErr(f.Index)
			}
		}
	}
	switch value.Kind() {
	case reflect.Map:
		child = mapValue(value, name)
	case reflect.Array, reflect.Slice:
		if index < value.Len() {
			child = value.Index(index)
		}
	}
	return shape, child, tag
}

func mapValue(value reflect.Value, name string) reflect.Value {
	key := reflect.New(value.Type().Key()).Elem()
	if key.Kind() == reflect.String {
		key.SetString(name)
		return value.MapIndex(key)
	}
	if key.Type().Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
		for iterator := value.MapRange(); iterator.Next(); {
			text, err := iterator.Key().Interface().(encoding.TextMarshaler).MarshalText()
			if err == nil && string(text) == name {
				return iterator.Value()
			}
		}
		return reflect.Value{}
	}
	if err := json.Unmarshal([]byte(name), key.Addr().Interface()); err != nil {
		return reflect.Value{}
	}
	return value.MapIndex(key)
}

func rewriteScalar(data []byte, value reflect.Value, output, legacyNull, nullable bool, tag reflect.StructTag, path string) ([]byte, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) && (legacyNull || tag.Get("nullable") == "true" || (nullable && tag.Get("nullable") != "false")) {
		return data, nil
	}
	if !output && value.IsValid() && (value.Kind() == reflect.Float32 || value.Kind() == reflect.Float64) {
		limit := float64(1 << 53)
		if value.Kind() == reflect.Float32 {
			limit = 1 << 24
		}
		if value.Float() >= limit || value.Float() <= -limit {
			return nil, fmt.Errorf("%s: use a decimal string or integer token outside the exact floating-point range", path)
		}
	}
	digits, err := Decimal(data)
	if err == nil && !output {
		err = checkInteger(digits, tag)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if output || strings.Contains(","+string(tag.Get("json"))+",", ",string,") {
		digits = strconv.Quote(digits)
	}
	return []byte(digits), nil
}
