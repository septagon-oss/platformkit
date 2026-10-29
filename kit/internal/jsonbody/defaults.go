package jsonbody

import (
	"fmt"
	"reflect"
)

// Defaults fills the engine's zero-valued defaults before its float-based
// conversion can replace them. The defaulted-command adapter owns omission itself.
func Defaults(value reflect.Value, shape *Shape) error {
	if shape == nil || shape.Opaque || !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return Defaults(value.Elem(), shape.Elem)
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		for i := range value.Len() {
			if err := Defaults(value.Index(i), shape.Elem); err != nil {
				return err
			}
		}
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	for _, f := range shape.Fields {
		field, err := value.FieldByIndexErr(f.Index)
		if err != nil || !field.CanAddr() {
			continue
		}
		if raw := f.Tag.Get("default"); raw != "" && field.IsZero() && hasInteger(f.Shape, map[*Shape]bool{}) {
			if err := Decode([]byte(raw), field.Addr().Interface()); err != nil {
				return fmt.Errorf("%s default: %w", f.Name, err)
			}
		}
		if err := Defaults(field, f.Shape); err != nil {
			return err
		}
	}
	return nil
}
