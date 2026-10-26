package resource

import (
	"cmp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
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
	words := o.Words(r.Schema)
	if r.Resolved == nil || r.Resolved.PolicyVersion != present.PolicyVersion {
		schema := r.Schema
		schema.Fields = words.Fields(schema.Fields)
		// A word is the resource's own, in the reader's language: the entity's declared
		// hints, then the two words the resource declaration carries, then the store's
		// name for the entity. Nothing appends a letter here — a plural with no source
		// is answered by the singular, which is the rule this package follows wherever
		// a count meets a noun.
		hints := words.Entry(r.Present)
		hints.Singular = cmp.Or(hints.Singular, r.Name.One, strings.ToLower(display.Humanize(schema.Entity)))
		hints.Plural = cmp.Or(hints.Plural, r.Name.Other, hints.Singular)
		commands := make([]present.CommandInput, 0, len(r.Commands))
		for _, c := range r.Commands {
			commands = append(commands, present.CommandInput{Verb: c.Verb, Summary: c.Label, Hints: words.Command(c.Verb, c.Present)})
		}
		language := "en"
		if o.Locale != nil {
			language = o.Locale.Language
		}
		policy := present.Resolve(present.Input{Schema: schema, Hints: hints, Immutable: r.Immutable,
			Singleton: r.Singleton, CommandsOnly: r.CommandsOnly, Operations: r.Operations, Commands: commands, Language: language,
			Text: PolicyText(func(key, fallback string) string { return o.Text(key, fallback, []any{}...) })})
		r.Resolved = &policy
	}
	// A word nobody declared — the label computed off a field's name, the field's own
	// doc sentence, an enum value's own spelling — is copy a person reads just the
	// same, so it is asked of the catalogue at the address it is known by, with the
	// computed word behind it. The resolver's own English is the fallback, which is
	// what a module that ships no copy of its own reads today.
	fields := r.Resolved.Fields
	for i, f := range fields {
		schema, ok := schemaFieldNamed(r.Schema.Fields, f.Name)
		if !ok || f.Label == display.FieldLabel(schema) && schema.Presentation.Label == "" {
			if named, isKernel := kernelWords[f.Name]; isKernel {
				fields[i].Label = words.sayLayer(named)
				continue
			}
			fields[i].Label = words.schemaWord("field", f.Name, f.Label)
		} else if schema.Presentation.Label == "" {
			fields[i].Label = words.schemaWord("field", f.Name, f.Label)
		}
		if schema.Presentation.Help == "" {
			fields[i].Help = words.schemaWord("field", f.Name+".help", f.Help)
		}
		if len(schema.Presentation.EnumLabels) == 0 {
			for j, option := range f.Options {
				fields[i].Options[j].Label = words.schemaWord("enum", option.Value, option.Label)
			}
		}
	}
	r.Resolved.Fields = fields

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

// schemaFieldNamed is the schema's own field for a resolved one, and whether the
// schema holds it at all: an undeclared word is asked under the field's address, and
// a resolved field with no schema field has none.
func schemaFieldNamed(fields []entity.Field, name string) (entity.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return entity.Field{}, false
}

// entryWroteWords reports whether the entry declared its own two words. It is asked
// here because this file is the one that reads the raw hints: a count either uses the
// pair the author wrote, already in the reader's language, or the resource name's own
// catalogue key, and nothing in between.
func (r Resource) entryWroteWords() bool {
	return r.Present.Singular != "" || r.Present.Plural != ""
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
