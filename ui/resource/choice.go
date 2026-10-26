package resource

import (
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

func (r Resource) choiceRead(o Options, f entity.Field, value any) g.Node {
	text := func(key, fallback string) string {
		if o.Locale == nil {
			return fallback
		}
		return o.Locale.Text(key, fallback)
	}
	r = r.policy(o)
	for _, resolved := range r.Resolved.Fields {
		if resolved.Name == f.Name {
			return forms.ChoiceRead(resolved, readChoiceIDs(f, value), o.PictureSource, o.ComposedSource, text)
		}
	}
	return nil
}

// readChoiceIDs is a stored value's answer as the ids the declaration knows; a
// value that names none reads as the empty answer, which only a write refuses.
func readChoiceIDs(f entity.Field, value any) []string {
	if value == nil {
		return []string{}
	}
	ids, err := f.Choices.ChoiceIDs(value)
	if err != nil {
		return []string{}
	}
	return ids
}

// choiceDefault is the field's declared default as chosen ids.
func choiceDefault(f entity.Field) []string {
	if f.Default == "" {
		return nil
	}
	value, err := entity.DefaultValue(f)
	if err != nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		_ = json.Unmarshal(raw, &value)
	}
	return readChoiceIDs(f, value)
}
