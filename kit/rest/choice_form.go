package rest

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
)

const choicePresent = "__pkit_choice_present["
const choiceOrder = "__pkit_choice_order["

func choiceMarkers(form url.Values, fields []entity.Field, refused []string) error {
	for key, values := range form {
		if !strings.HasPrefix(key, "__pkit_choice_") {
			continue
		}
		prefix := choicePresent
		if strings.HasPrefix(key, choiceOrder) {
			prefix = choiceOrder
		}
		name, ok := strings.CutPrefix(key, prefix)
		if !ok || !strings.HasSuffix(name, "]") {
			return invalid(key, "unknown choice transport marker")
		}
		name = strings.TrimSuffix(name, "]")
		f, found := entity.FieldNamed(fields, name)
		if !found || f.Choices == nil || entity.ServerOwned(f) || slices.Contains(refused, name) {
			return invalid(name, "choice marker names no writable choice")
		}
		if len(values) != 1 || (prefix == choicePresent && values[0] != "1") {
			return invalid(name, "supply one valid choice marker")
		}
		if prefix == choiceOrder {
			_, named := form[name]
			_, present := form[choicePresent+name+"]"]
			if f.Choices.Cardinality != "many" || (!named && !present) {
				return invalid(name, "choice order requires a submitted many-choice field")
			}
		}
	}
	return nil
}

func readChoice(form url.Values, f entity.Field, update bool, out map[string]any) error {
	raw, named := form[f.Name]
	_, present := form[choicePresent+f.Name+"]"]
	if !named && !present {
		return nil
	}
	c := f.Choices
	ids := append([]string{}, raw...)
	if c.Cardinality == "one" && len(ids) == 1 && ids[0] == "" {
		ids = []string{}
	}
	// Validate all submitted IDs before exclusivity or replacement can discard one.
	checked := *c.Clone()
	checked.Empty.Mode, checked.Empty.Message, checked.Maximum, checked.NoPreference = "allow", "", nil, ""
	if _, err := checked.Answer(ids); err != nil {
		return invalid(f.Name, err.Error())
	}
	if c.Cardinality == "many" {
		ordered, err := nativeChoiceOrder(form, f, ids)
		if err != nil {
			return err
		}
		ids = ordered
	}
	if c.NoPreference != "" && slices.Contains(ids, c.NoPreference) {
		ids = []string{c.NoPreference}
	}
	var err error
	if update {
		ids, err = c.Change(nil, ids)
	} else {
		ids, err = c.Answer(ids)
	}
	if err != nil {
		return invalid(f.Name, err.Error())
	}
	out[f.Name] = choiceValue(c, ids)
	return nil
}

func nativeChoiceOrder(form url.Values, f entity.Field, checked []string) ([]string, error) {
	order := []string{}
	if raw, ok := form[choiceOrder+f.Name+"]"]; ok {
		if len(raw) != 1 || json.Unmarshal([]byte(raw[0]), &order) != nil || order == nil {
			return nil, invalid(f.Name, "choice order must be an array of IDs")
		}
		c := *f.Choices.Clone()
		c.Empty.Mode, c.Empty.Message, c.Maximum, c.NoPreference = "allow", "", nil, ""
		if _, err := c.Answer(order); err != nil {
			return nil, invalid(f.Name, err.Error())
		}
	}
	order = slices.DeleteFunc(order, func(id string) bool { return !slices.Contains(checked, id) })
	for _, o := range f.Choices.DisplayOptions() {
		if slices.Contains(checked, o.ID) && !slices.Contains(order, o.ID) {
			order = append(order, o.ID)
		}
	}
	return order, nil
}
