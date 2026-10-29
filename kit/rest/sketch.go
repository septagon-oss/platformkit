package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// patchValues decodes drawings through their exact tuple codec while preserving
// ordinary fields' existing JSON number handling.
type patchValues[T any] map[string]any

func (v *patchValues[T]) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(patchValues[T], len(raw))
	fields := entity.FieldsOf(reflect.TypeFor[T]())
	for name, data := range raw {
		f, _ := entity.FieldNamed(fields, name)
		if f.Type == entity.TypeSketch {
			var drawing entity.Sketch
			if err := drawing.UnmarshalJSON(data); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			out[name] = drawing
		} else {
			value, err := patchFieldValue(data)
			if err != nil {
				return err
			}
			out[name] = value
		}
	}
	*v = out
	return nil
}

// patchFieldValue decodes one submitted field of a patch body, keeping a number as
// the token the body wrote. merge carries these values into the entity through the
// same codec the request body went through, which refuses a float64 whose precision
// cannot establish an exact int64: an untyped float64 here would ask a caller who
// sent exact digits to send them again as a string.
func patchFieldValue(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	err := decoder.Decode(&value)
	return value, err
}

func sketchDeclarationFault(t reflect.Type, fields []entity.Field) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for _, f := range fields {
		if bad := f.SketchFault(); bad != "" {
			return bad
		}
		if f.Type != entity.TypeSketch {
			continue
		}
		sf := t.FieldByIndex(f.Index)
		for _, tag := range []string{"default", "enum", "format", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "minItems", "maxItems"} {
			if _, ok := sf.Tag.Lookup(tag); ok {
				return fmt.Sprintf("sketch field %q cannot declare %s", f.Name, tag)
			}
		}
		for part := range strings.SplitSeq(sf.Tag.Get("json"), ",") {
			if part == "omitempty" || part == "omitzero" {
				return fmt.Sprintf("sketch field %q must preserve null and [] without %s", f.Name, part)
			}
		}
		for part := range strings.SplitSeq(sf.Tag.Get("validate"), ",") {
			if part != "" && part != "required" {
				return fmt.Sprintf("sketch field %q supports only required validation", f.Name)
			}
		}
	}
	return ""
}

func sketchFormValue(f entity.Field, raw []string) (any, error) {
	if len(raw) != 1 || strings.TrimSpace(raw[0]) == "" {
		return nil, invalid(f.Name, "Submit one drawing as JSON.")
	}
	var value entity.Sketch
	if err := value.UnmarshalJSON([]byte(raw[0])); err != nil {
		return nil, invalid(f.Name, err.Error())
	}
	if f.Required && len(value) == 0 {
		return nil, invalid(f.Name, "Add a drawing before continuing.")
	}
	return value, nil
}

func validateSketchInput(value any) error {
	if err := entity.ValidateSketchFields(value); err != nil {
		return fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	return nil
}

// Resolve keeps semantic decoding errors attached to their field even when the
// framework also reports the failed typed body as a whole.
func (in *bodyInput[T]) Resolve(huma.Context) []error {
	var raw map[string]json.RawMessage
	if json.Unmarshal(in.RawBody, &raw) != nil {
		return nil
	}
	var out []error
	for _, f := range entity.FieldsOf(reflect.TypeFor[T]()) {
		if f.Type != entity.TypeSketch {
			continue
		}
		for name, data := range raw {
			if !strings.EqualFold(name, f.Name) {
				continue
			}
			var drawing entity.Sketch
			if err := drawing.UnmarshalJSON(data); err != nil {
				out = append(out, &huma.ErrorDetail{Location: "body." + f.Name, Message: err.Error()})
			} else if f.ReadOnly {
				out = append(out, &huma.ErrorDetail{Location: "body." + f.Name, Message: "This drawing is read-only."})
			}
		}
	}
	return out
}

func (s Singleton[T]) save(ctx context.Context, tx db.Tx[db.Tenant], value T) (T, error) {
	if err := validateSketchInput(value); err != nil {
		var zero T
		return zero, err
	}
	return s.Save(ctx, tx, value)
}
