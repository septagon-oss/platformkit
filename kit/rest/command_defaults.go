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
	prepare := func(values map[string]any) (I, error) {
		var in I
		if reflect.TypeFor[I]().Kind() == reflect.Pointer {
			reflect.ValueOf(&in).Elem().Set(reflect.New(reflect.TypeFor[I]().Elem()))
		}
		patch := make(map[string]any, len(defaults)+len(values))
		for name, value := range defaults {
			patch[name] = value
		}
		for name, value := range values {
			patch[name] = value
		}
		target := any(&in)
		if reflect.TypeFor[I]().Kind() == reflect.Pointer {
			target = any(in)
		}
		_, err := merge(target, fields, nil, patch)
		return in, err
	}
	if _, err := prepare(nil); err != nil {
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
	for _, sf := range reflect.VisibleFields(in) {
		if !sf.IsExported() || jsonNameOf(sf) == "-" {
			continue
		}
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		embedded := sf.Type
		if embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		if sf.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
			continue
		}
		if _, ok := entity.FieldNamed(fields, jsonNameOf(sf)); !ok {
			return fmt.Sprintf("argument %q is absent from the input schema", jsonNameOf(sf))
		}
	}
	return ""
}
