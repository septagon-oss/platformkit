package entity

import (
	"fmt"
	"reflect"
)

// ValidateSketchFields checks reflected drawing values and their requiredness
// before an owning command performs effects. A nil container has no fields.
func ValidateSketchFields(value any) error {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	for _, f := range FieldsOf(v.Type()) {
		if f.Type != TypeSketch {
			continue
		}
		field, err := v.FieldByIndexErr(f.Index)
		var s Sketch
		if err != nil {
			field = reflect.ValueOf(Sketch(nil))
		}
		if field.Kind() == reflect.Pointer {
			if !field.IsNil() {
				s = field.Elem().Interface().(Sketch)
			}
		} else {
			s = field.Interface().(Sketch)
		}
		if err := s.Validate(); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		if f.Required && len(s) == 0 {
			return fmt.Errorf("%s: %w", f.Name, sketchError("Add a drawing before continuing."))
		}
	}
	return nil
}

// SketchFault reports unsupported drawing metadata, including a sketch hint on
// another type. Empty means the declaration can be rendered without coercion.
func (f Field) SketchFault() string {
	if f.Type != TypeSketch {
		if f.Widget == "sketch" || f.Present == "sketch" {
			return fmt.Sprintf("field %q needs type sketch", f.Name)
		}
		return ""
	}
	// A drawing is read as a drawing because of its type, so it carries no control
	// name beyond `sketch` and no read name at all; `present:` names a reading the
	// type does not already give, and for this type it cannot.
	if f.Widget != "" && f.Widget != "sketch" || f.Present != "" ||
		len(f.Enum) > 0 || f.Default != "" || f.MaxLength != 0 || f.Display || f.Elem != "" ||
		f.Presentation.Format != "" || f.Presentation.Money != nil || f.Presentation.Reference != nil {
		return fmt.Sprintf("sketch field %q cannot carry scalar hints, defaults or a different presentation", f.Name)
	}
	return ""
}

// SketchFault refuses scalar and image roles for drawings. Drawing value slots,
// labels and visibility are supported independently of these entry roles.
func (p EntryHints) SketchFault(fields []Field) string {
	names := []string{p.PrimaryField, p.PreviewField, p.StatusField, p.CoverField, p.ThumbnailField, p.TrendField}
	names = append(names, p.Sortable...)
	if p.Highlight != nil {
		for _, filter := range p.Highlight.Filter {
			names = append(names, filter.Field)
		}
	}
	for _, name := range names {
		if f, ok := FieldNamed(fields, name); ok && f.Type == TypeSketch {
			return fmt.Sprintf("sketch field %q cannot be used as scalar text, an image or a query key", name)
		}
	}
	return ""
}
