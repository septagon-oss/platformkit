package resource

import (
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// PolicyText adapts the resolver's vocabulary to the screen catalogue. A nil
// formatter retains the resolver's English defaults.
func PolicyText(text Text) Text {
	if text == nil {
		return nil
	}
	return func(key, fallback string) string {
		switch key {
		case "present.form.details":
			key = "forms.step.details"
		case "present.missing":
			key = "screens.missing"
		case "present.true":
			key = "screens.true"
		case "present.false":
			key = "screens.false"
		case "present.emptyTitle":
			key = "screens.empty_title"
		}
		return text(key, fallback)
	}
}

// policy accepts a supported block whole; legacy inputs resolve once per render.
func (r Resource) policy(o Options) Resource {
	if r.Resolved == nil || r.Resolved.PolicyVersion != present.PolicyVersion {
		words := o.Words(r.Schema)
		schema := r.Schema
		schema.Fields = words.Fields(schema.Fields)
		commands := make([]present.CommandInput, 0, len(r.Commands))
		for _, c := range r.Commands {
			// The command's own shape travels with its words: which arguments it takes,
			// which the screen fills in, and whether one form or many name them.
			commands = append(commands, c.input(words.Command(c.Verb, c.Present)))
		}
		language := "en"
		if o.Locale != nil {
			language = o.Locale.Language
		}
		policy := present.Resolve(present.Input{Schema: schema, Hints: words.Entry(r.Present), Immutable: r.Immutable,
			Singleton: r.Singleton, CommandsOnly: r.CommandsOnly, Operations: r.Operations, Commands: commands, Language: language,
			Text: PolicyText(func(key, fallback string) string { return o.Text(key, fallback, []any{}...) })})
		r.Resolved = &policy
	}
	commands := make([]Command, 0, len(r.Commands))
	for _, resolved := range r.Resolved.Commands {
		if !resolved.Visible || resolved.System {
			continue
		}
		for _, c := range r.Commands {
			if c.Verb == resolved.Verb {
				c.Resolved = new(resolved)
				c.Label = resolved.Label
				commands = append(commands, c)
				break
			}
		}
	}
	slices.SortStableFunc(commands, func(a, b Command) int {
		if a.Resolved.Primary == b.Resolved.Primary {
			return 0
		}
		if a.Resolved.Primary {
			return -1
		}
		return 1
	})
	r.Commands = commands
	return r
}

// recordForm is the create or edit form of the record itself, its fields drawn but kept
// still. Which verbs are mounted decides which screens exist and which writes their
// controls may start, since a draft address is a route's address; the fields belong to
// the record, so a form beside no write route shows that record with nothing typable on
// it. Only a render holding no route table comes here: a mounted form screen 404s first.
func (r Resource) recordForm(kind, verb string) present.ResolvedView {
	in := present.Input{Schema: r.Schema, Hints: r.Present, Immutable: r.Immutable, Singleton: r.Singleton,
		CommandsOnly: r.CommandsOnly, Operations: append(slices.Clone(r.Operations), verb)}
	for _, v := range present.Resolve(in).Views {
		if v.Kind != kind {
			continue
		}
		for i, c := range v.Controls {
			if c.Name == "field" {
				c.Editable, c.Interactions = false, nil
				v.Controls[i] = c
			}
		}
		return v
	}
	return present.ResolvedView{}
}

func (r Resource) field(name string) present.ResolvedField {
	for _, f := range r.Resolved.Fields {
		if f.Name == name {
			return f
		}
	}
	return present.ResolvedField{Name: name}
}

func (r Resource) primaryAction(collection bool) string {
	for _, c := range r.Commands {
		if c.Collection == collection && c.Resolved != nil && c.Resolved.Primary {
			return "outline"
		}
	}
	return ""
}

func resolvedFormField(f entity.Field, p present.ResolvedField, o Options) forms.Field {
	f.Doc = p.Help
	// The control the resolver names is the one the form draws — except where
	// its only reason is the column's storage type. A `text` column is not its
	// author asking for a five-row box, and an email address was one. Every
	// other answer stays as the resolver gave it, and ui/forms still reads the
	// field's type for the input it draws.
	if p.Control != "textarea" || f.Widget != "" {
		f.Widget = p.Control
	}
	out := forms.Field{Resolved: &p, PictureSource: o.PictureSource, ComposedSource: o.ComposedSource,
		Definition: f, Label: p.Label, Help: p.Help}
	// A choice carries its answers in the resolved policy the control reads, and
	// may not also declare an enum, so its field keeps no enum of its own.
	if f.Choices != nil {
		return out
	}
	f.Enum = make([]string, 0, len(p.Options))
	options := make([]components.SelectOption, 0, len(p.Options))
	for _, option := range p.Options {
		f.Enum = append(f.Enum, option.Value)
		options = append(options, components.SelectOption{Value: option.Value, Label: option.Label})
	}
	out.Definition, out.Options = f, options
	return out
}

func (w Words) field(f entity.Field) present.ResolvedField {
	schema := w.schema
	schema.Fields = w.Fields([]entity.Field{f})
	return present.Resolve(present.Input{Schema: schema, Text: PolicyText(w.text)}).Fields[0]
}
