package resource

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

func (r Resource) policy(o Options) *present.Resolved {
	if r.Resolved != nil {
		return r.Resolved
	}
	schema := r.Schema
	schema.Fields = o.Words(schema).Fields(schema.Fields)
	language := "en"
	if o.Locale != nil {
		language = o.Locale.Language
	}
	resolved := present.Resolve(present.Input{Schema: schema, Hints: r.Present, Immutable: r.Immutable, Singleton: r.Singleton, Operations: r.Operations, Language: language})
	return &resolved
}

func (r Resource) choiceRead(o Options, f entity.Field, value any) g.Node {
	text := func(key, fallback string) string {
		if o.Locale == nil {
			return fallback
		}
		return o.Locale.Text(key, fallback)
	}
	for _, resolved := range r.policy(o).Fields {
		if resolved.Name == f.Name {
			return forms.ChoiceRead(resolved, readChoiceIDs(f, value), o.PictureSource, o.ComposedSource, text)
		}
	}
	return nil
}
