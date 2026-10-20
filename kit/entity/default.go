package entity

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DefaultValue decodes a nonempty explicit default using the field's metadata.
// Composite values remain immutable JSON bytes for the typed adapter to decode
// freshly per invocation. Invalid types, enum values and map keys return errors.
func DefaultValue(f Field) (any, error) {
	invalid := func(why string) (any, error) {
		return nil, fmt.Errorf("command argument %q default %q: %s", f.Name, f.Default, why)
	}
	if f.Type != TypeList && f.Type != TypeMap {
		value, err := Coerce(f, f.Default)
		if err != nil {
			return invalid(err.Error())
		}
		if len(f.Enum) > 0 && !slices.Contains(f.Enum, f.Default) {
			return invalid("value is outside the enum")
		}
		return value, nil
	}
	raw := strings.TrimSpace(f.Default)
	opening := byte('[')
	if f.Type == TypeMap {
		opening = '{'
	}
	if len(raw) == 0 || raw[0] != opening || !json.Valid([]byte(raw)) {
		return invalid("expected a JSON " + string(f.Type))
	}
	var elements []json.RawMessage
	if f.Type == TypeMap {
		var values map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return invalid(err.Error())
		}
		for _, key := range slices.Sorted(maps.Keys(values)) {
			if !slices.ContainsFunc(f.Keys, func(k MapKey) bool { return k.Name == key }) {
				return invalid("unknown map key " + key)
			}
			elements = append(elements, values[key])
		}
	} else {
		if err := json.Unmarshal([]byte(raw), &elements); err != nil {
			return invalid(err.Error())
		}
	}
	for _, element := range elements {
		literal := string(element)
		if f.Elem == TypeString || f.Elem == TypeText || f.Elem == TypeUUID || f.Elem == TypeTime {
			if len(literal) == 0 || literal[0] != '"' {
				return invalid("element is not a " + string(f.Elem))
			}
			if err := json.Unmarshal(element, &literal); err != nil {
				return invalid(err.Error())
			}
		}
		if !scalarField(Field{Type: f.Elem}) {
			return invalid("unsupported element type " + string(f.Elem))
		}
		if _, err := Coerce(Field{Name: f.Name, Type: f.Elem}, literal); err != nil {
			return invalid(err.Error())
		}
	}
	return json.RawMessage(raw), nil
}
