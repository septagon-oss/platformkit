package rest

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// commandDefaults audits typed input and prepares each invocation independently.
// Invalid declared defaults refuse registration; missing defaults only make the
// input ineligible for a deck. Supplied values always override defaults.
func commandDefaults[I any](fields []crud.Field) (func(map[string]any) (I, error), string, error) {
	defaults := map[string]any{}
	fault := inputSchemaFault(reflect.TypeFor[I](), fields)
	for _, f := range fields {
		if f.Default == "" {
			if fault == "" {
				fault = fmt.Sprintf("argument %q has no explicit default", f.Name)
			}
			continue
		}
		value, err := entity.DefaultValue(f)
		if err != nil {
			return nil, "", err
		}
		defaults[f.Name] = value
	}
	if _, err := decodeCommandValues[I](fields, defaults); err != nil {
		return nil, "", fmt.Errorf("command defaults: %w", err)
	}
	prepare := func(values map[string]any) (I, error) {
		patch := make(map[string]any, len(defaults)+len(values))
		for name, value := range defaults {
			patch[name] = value
		}
		for name, value := range values {
			patch[name] = value
		}
		patch, err := normalizeChoices(fields, patch, nil, true)
		if err != nil {
			var zero I
			return zero, err
		}
		return decodeCommandValues[I](fields, patch)
	}
	if _, err := prepare(nil); err != nil {
		if hasChoices(fields) {
			return prepare, err.Error(), nil
		}
		return nil, "", fmt.Errorf("command defaults: %w", err)
	}
	return prepare, fault, nil
}

func inputSchemaFault(in reflect.Type, fields []crud.Field) string {
	if in.Kind() == reflect.Pointer {
		in = in.Elem()
	}
	if in.Kind() != reflect.Struct {
		return "input is not a declared struct"
	}
	// Go name collisions can hide promoted fields from VisibleFields even when
	// distinct JSON tags expose them. Walk embeddings without that name filter.
	pending := []reflect.Type{in}
	seen := map[reflect.Type]bool{}
	for len(pending) > 0 {
		typ := pending[0]
		pending = pending[1:]
		if seen[typ] {
			continue
		}
		seen[typ] = true
		for i := range typ.NumField() {
			sf := typ.Field(i)
			if jsonNameOf(sf) == "-" {
				continue
			}
			name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
			embedded := sf.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if sf.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
				pending = append(pending, embedded)
				continue
			}
			if sf.IsExported() {
				if _, ok := entity.FieldNamed(fields, jsonNameOf(sf)); !ok {
					return fmt.Sprintf("argument %q is absent from the input schema", jsonNameOf(sf))
				}
			}
		}
	}
	return ""
}

func decodeCommandValues[I any](fields []crud.Field, values map[string]any) (I, error) {
	var in I
	target := any(&in)
	if reflect.TypeFor[I]().Kind() == reflect.Pointer {
		reflect.ValueOf(&in).Elem().Set(reflect.New(reflect.TypeFor[I]().Elem()))
		target = any(in)
	}
	_, err := merge(target, fields, nil, values)
	return in, err
}
