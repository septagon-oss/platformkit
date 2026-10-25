package rest

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func (s Spec[T]) choiceFields() []entity.Field {
	fields, err := entity.WithChoices(crud.Fields[T](), s.Choices)
	if err == nil {
		err = choiceTypes(reflect.TypeFor[T](), fields)
	}
	if err != nil {
		panic("rest: Spec for " + s.Module + "." + s.Entity + ": " + err.Error())
	}
	return fields
}

func choiceTypes(typ reflect.Type, fields []entity.Field) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for _, f := range fields {
		if f.Choices == nil {
			continue
		}
		t := typ.FieldByIndex(f.Index).Type
		if t.Kind() == reflect.Pointer {
			return fmt.Errorf("choice field %q must not be a pointer", f.Name)
		}
	}
	return nil
}

func hasChoices(fields []entity.Field) bool {
	return slices.ContainsFunc(fields, func(f entity.Field) bool { return f.Choices != nil })
}

func (s Spec[T]) choiceFault() string {
	fields := s.choiceFields()
	for _, f := range fields {
		if f.Choices == nil {
			continue
		}
		if s.Present.StatusField == f.Name {
			return "a choice field cannot also be the status field"
		}
		if s.offers(Update) && !entity.ServerOwned(f) && !slices.Contains(s.Immutable, f.Name) && revisionColumn(fields) == "" {
			return "editable choices require a revision field"
		}
	}
	return ""
}

func choiceValue(c *entity.ChoiceSet, ids []string) any {
	if c.Cardinality == "one" {
		if len(ids) > 0 {
			return ids[0]
		}
		return ""
	}
	return ids
}

// normalizeChoices is shared by JSON, native forms and in-process commands.
// Updates omit unchanged choices only after the caller has checked authority.
func normalizeChoices(fields []entity.Field, values map[string]any, current map[string]any, create bool) (map[string]any, error) {
	out := maps.Clone(values)
	if out == nil {
		out = map[string]any{}
	}
	for _, f := range fields {
		if f.Choices == nil || entity.ServerOwned(f) {
			continue
		}
		value, sent := values[f.Name]
		if !sent && !create {
			continue
		}
		if !sent {
			value = choiceValue(f.Choices, []string{})
			if f.Default != "" {
				var err error
				value, err = entity.DefaultValue(f)
				if err != nil {
					return nil, err
				}
			}
		}
		if raw, ok := value.(json.RawMessage); ok {
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, invalid(f.Name, err.Error())
			}
		}
		ids, err := f.Choices.ChoiceIDs(value)
		if err != nil {
			return nil, invalid(f.Name, err.Error())
		}
		var answer, before []string
		if create {
			answer, err = f.Choices.Answer(ids)
		} else {
			if current[f.Name] != nil {
				before, err = f.Choices.ChoiceIDs(current[f.Name])
				if err != nil {
					return nil, invalid(f.Name, err.Error())
				}
			}
			answer, err = f.Choices.Change(before, ids)
		}
		if err != nil {
			return nil, invalid(f.Name, err.Error())
		}
		if !create && slices.Equal(before, answer) {
			delete(out, f.Name)
		} else {
			out[f.Name] = choiceValue(f.Choices, answer)
		}
	}
	return out, nil
}

func (s Spec[T]) choiceCreate(e T, values map[string]any) (T, error) {
	if !hasChoices(s.choiceFields()) {
		return e, nil
	}
	normalized, err := normalizeChoices(s.choiceFields(), values, nil, true)
	if err != nil {
		var zero T
		return zero, err
	}
	return decode[T](normalized)
}

func choiceCondition(fields []entity.Field, immutable []string, quoted string) error {
	for _, f := range fields {
		if f.Choices != nil && !entity.ServerOwned(f) && !slices.Contains(immutable, f.Name) && quoted == "" {
			return invalid(entity.RevisionCondition, "quote the revision before updating choices")
		}
	}
	return nil
}

func (s Spec[T]) choiceCreateBody(body []byte, e T) (T, error) {
	if !hasChoices(s.choiceFields()) {
		return e, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(body, &values); err != nil {
		var zero T
		return zero, err
	}
	return s.choiceCreate(e, choiceRawValues(values))
}

func (s Spec[T]) choiceCreateRow(e T) (T, error) {
	if !hasChoices(s.choiceFields()) {
		return e, nil
	}
	values, err := jsonSnapshot(e)
	if err != nil {
		var zero T
		return zero, err
	}
	for _, f := range s.choiceFields() {
		if f.Choices != nil && string(values[f.Name]) == "null" {
			delete(values, f.Name)
		}
	}
	return s.choiceCreate(e, choiceRawValues(values))
}

func choiceRawValues(raw map[string]json.RawMessage) map[string]any {
	values := make(map[string]any, len(raw))
	for name, value := range raw {
		values[name] = value
	}
	return values
}
