package resource

import (
	"maps"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

// Declared words are localized before policy resolution. Keys use
// hints.<module>/<entity>.<aspect>; machine names and values stay unchanged.
// See README.md for the vocabulary and fallback contract.

// Text is the seam a declared string is resolved through: the same shape
// Options.Text already is, seen from the side that has a schema and no
// render — which is what lets ui/screens (the catalogue document) and
// ui/resource (the generated screens) share one resolver.
type Text func(key, fallback string) string

// Words is one resource's declared reading in one request's language. A zero
// Words — no text seam — answers every lookup with the declared literal, which
// is what "no catalog is composed" has always meant here.
type Words struct {
	schema entity.Schema
	text   Text
	// phrase is the same catalogue seen from the side that interpolates: a count, a
	// measurement or an instant is copy in its layout as well as its words. A nil
	// phrase leaves every number and instant as kit/entity/display writes it.
	phrase func(key, fallback string, args ...any) string
	// prefix holds an aspect path inside the resource: empty for the resource's
	// own declarations, `command.assign.` for one command's. It is why a command
	// argument's label is `…command.assign.field.assigneeId.label` and the field of
	// the same name on the row is `…field.assigneeId.label` — one argument of one
	// command and one column of every row are two strings a translator may want to
	// choose differently.
	prefix string
}

// WordsFor is one resource's words through the given seam; pass nil for the
// declaration as written.
func WordsFor(schema entity.Schema, text Text) Words {
	return Words{schema: schema, text: text}
}

// Key is the copy key of one aspect of this resource's declaration. It is
// exported because a translation is written by hand into a module's message
// file, and this is the only function that knows the spelling.
func (w Words) Key(aspect string) string {
	return "hints." + w.schema.Module + "/" + w.schema.Entity + "." + w.prefix + aspect
}

// forCommand is the same resource's words, read under one command's prefix.
func (w Words) forCommand(verb string) Words {
	scoped := w
	scoped.prefix = "command." + verb + "."
	return scoped
}

// Section is one declared block of the record screen, its label in this
// request's language and its key — which is what a field's `section:` names —
// untouched.
func (w Words) Section(sec entity.EntitySection) entity.EntitySection {
	sec.Label = w.say("section."+sec.Key, sec.Label)
	return sec
}

// say is the fallback rule in one place, so two call sites cannot write it
// differently: the translation, unless it comes back empty, in which case the
// declared word. `xtext` cannot answer empty (its loader refuses a copy whose
// translation is blank), so this guard is against a foreign Formatter, and the
// cost of not having it is a label that reads as nothing.
func (w Words) say(aspect, declared string) string {
	if declared == "" || w.text == nil {
		return declared
	}
	if out := w.text(w.Key(aspect), declared); out != "" {
		return out
	}
	return declared
}

// Entry returns declared entry words in this request's language.
// Mutable blocks are copied before localization; deck queue and directions are
// always copied so callers may safely narrow them to authorized commands.
func (w Words) Entry(p entity.EntryHints) entity.EntryHints {
	out := p
	out.Deck = p.Deck.Clone()
	if out.Deck != nil {
		for key, d := range out.Deck.Directions {
			if !d.Skip {
				d.Label = w.say("deck."+key+".label", d.Label)
				out.Deck.Directions[key] = d
			}
		}
	}
	if w.text == nil {
		return out
	}
	out.Singular = w.say("singular", p.Singular)
	out.Plural = w.say("plural", p.Plural)
	out.Description = w.say("description", p.Description)
	out.EmptyDescription = w.say("emptyDescription", p.EmptyDescription)
	if p.Group != nil {
		group := *p.Group
		group.Label = w.say("group."+group.Key, group.Label)
		out.Group = &group
	}
	if p.Highlight != nil {
		// The label is a word and the filter is not: which rows a sub-count counts
		// is a question the server asks, and a translated operator would answer a
		// different one. The predicates stay shared — nothing writes one.
		highlight := *p.Highlight
		highlight.Label = w.say("highlight.label", highlight.Label)
		out.Highlight = &highlight
	}
	if p.Sections != nil {
		sections := make([]entity.EntitySection, 0, len(p.Sections))
		for _, section := range p.Sections {
			sections = append(sections, w.Section(section))
		}
		out.Sections = sections
	}
	return out
}

// Fields is a schema with every declared word on it read in the request's
// language, and every non-word left exactly as the schema holds it. The slice
// and the two maps are copied; the caller's schema is never written to.
func (w Words) Fields(fields []entity.Field) []entity.Field {
	if w.text == nil || fields == nil {
		return fields
	}
	out := make([]entity.Field, 0, len(fields))
	for _, f := range fields {
		if label, help := f.Presentation.Label, f.Presentation.Help; label != "" || help != "" {
			f.Presentation.Label = w.say("field."+f.Name+".label", label)
			f.Presentation.Help = w.say("field."+f.Name+".help", help)
		}
		if len(f.Presentation.EnumLabels) > 0 {
			words := maps.Clone(f.Presentation.EnumLabels)
			for value, word := range words {
				words[value] = w.say("enum."+f.Name+"."+value, word)
			}
			f.Presentation.EnumLabels = words
		}
		if f.Choices != nil {
			f.Choices = f.Choices.Clone()
			prefix := "choice." + f.Name + "."
			for i := range f.Choices.Options {
				option := &f.Choices.Options[i]
				option.Label = w.say(prefix+option.ID+".label", option.Label)
				if option.Group != nil {
					option.Group.Label = w.say(prefix+"group."+option.Group.Key+".label", option.Group.Label)
				}
			}
			if f.Choices.Empty.Message != "" {
				f.Choices.Empty.Message = w.say(prefix+"empty", f.Choices.Empty.Message)
			}
			if m := f.Choices.Maximum; m != nil && m.Message != "" {
				m.Message = w.say(prefix+"maximum", m.Message)
			}
		}
		out = append(out, f)
	}
	return out
}

// Command is one command's description in the request's language — its label, its
// confirmation and its success message, under `command.<verb>.`.
func (w Words) Command(verb string, p entity.CommandHints) entity.CommandHints {
	if w.text == nil {
		return p
	}
	c := w.forCommand(verb)
	out := entity.CommandHints{
		Label:   c.say("label", p.Label),
		Primary: p.Primary, Destructive: p.Destructive, System: p.System,
		SuccessMessage: c.say("successMessage", p.SuccessMessage),
	}
	if p.Confirmation != nil {
		confirm := *p.Confirmation
		confirm.Title = c.say("confirmation.title", confirm.Title)
		confirm.Body = c.say("confirmation.body", confirm.Body)
		confirm.ConfirmLabel = c.say("confirmation.confirmLabel", confirm.ConfirmLabel)
		out.Confirmation = &confirm
	}
	return out
}

// FieldLabel returns the resolved label for one field.
//
// Deprecated: pass Resource.Resolved to the screen renderer.
func (w Words) FieldLabel(f entity.Field) string {
	if f.Presentation.Label == "" {
		if named, ok := kernelWords[f.Name]; ok {
			return w.sayLayer(named)
		}
		return w.schemaWord("field", f.Name, display.FieldLabel(f))
	}
	return w.field(f).Label
}

// FieldHelp returns the resolved help for one field.
//
// Deprecated: pass Resource.Resolved to the screen renderer.
func (w Words) FieldHelp(f entity.Field) string {
	if f.Presentation.Help == "" {
		return w.schemaWord("field", f.Name+".help", display.FieldHelp(f))
	}
	return w.field(f).Help
}

// EnumWord returns a resolved option label, or the unknown raw value.
//
// Deprecated: pass Resource.Resolved to the screen renderer.
func (w Words) EnumWord(f entity.Field, value string) string {
	for _, option := range w.field(f).Options {
		if option.Value == value {
			return option.Label
		}
	}
	return value
}

// Value renders one scalar without sibling values or a clock.
//
// Deprecated: use resource.Value with a resolved field, row and Options.
func (w Words) Value(f entity.Field, v any) string {
	if v != nil {
		switch f.Type {
		case entity.TypeTime:
			return w.moment(v)
		case entity.TypeBool:
			return w.boolean(v)
		case entity.TypeInt, entity.TypeFloat:
			return w.number(v)
		}
	}
	if w.text == nil || len(f.Enum) == 0 {
		return value(w.field(f), map[string]any{f.Name: v}, Options{}).text
	}
	return w.schemaWord("enum", display.Text(v), display.EnumWord(f, display.Text(v)))
}

// CommandLabel localizes an explicitly declared label, returning empty otherwise.
//
// Deprecated: read the command's label from Resource.Resolved.
func (w Words) CommandLabel(verb string, p entity.CommandHints) string {
	if p.Label == "" {
		return ""
	}
	return w.forCommand(verb).say("label", p.Label)
}

// CommandFields is one command's argument list in the request's language, under
// that command's prefix.
func (w Words) CommandFields(verb string, fields []entity.Field) []entity.Field {
	return w.forCommand(verb).Fields(fields)
}

// schemaWord asks the catalogue for a word nobody declared — the label computed off
// a field's name, a field's own doc sentence, an enum value's own spelling — so copy
// a screen invents is translatable too. The scoped key comes first:
// `field.<module>.<entity>.<field>`, with the field's bare `field.<field>` behind it,
// because two entities' "title" are two words. The computed string is the fallback of
// both, which is what a module that ships no copy of its own reads as today.
func (w Words) schemaWord(kind, name, computed string) string {
	if computed == "" || w.text == nil {
		return computed
	}
	said := func(key string) string {
		out := w.text(key, computed)
		if out == "" {
			return computed
		}
		return out
	}
	if w.schema.Module == "" || w.schema.Entity == "" {
		return said(kind + "." + name)
	}
	if out := said(kind + "." + w.schema.Module + "." + w.schema.Entity + "." + name); out != computed {
		return out
	}
	return said(kind + "." + name)
}

// RouteWords are a command's own two words: the Summary written beside the route,
// which is the word on the button, and the Description, which names the form. Neither
// is a `presentation` declaration, so this file's `command.<verb>.` keys never reach
// them, and a form that printed them as written named the action in the language the
// API was declared in.
func (w Words) RouteWords(verb, summary, description string) (string, string) {
	if w.text == nil || w.schema.Module == "" || w.schema.Entity == "" {
		return summary, description
	}
	at := "command." + w.schema.Module + "." + w.schema.Entity + "." + verb
	say := func(key, declared string) string {
		if out := w.text(key, declared); out != "" && out != declared {
			return out
		}
		return declared
	}
	return say(at, summary), say(at+".description", description)
}

// layerWord is one of this package's own words: its key and the English it falls back to.
type layerWord struct{ key, text string }

// kernelWords are the two columns a generated screen labels and no entity declares:
// the timestamps on every entity's base. A `presentation` label written there would
// put a word on the catalogue document of every resource, hinted or not, so the layer
// that draws the column owns the two words and asks them as it draws.
var kernelWords = map[string]layerWord{
	"createdAt": {"screens.created_at", "Created at"},
	"updatedAt": {"screens.updated_at", "Updated at"},
}

// sayLayer asks one of this package's own words under its unqualified key rather than
// under this resource's `hints.` prefix: the difference is who wrote the word. An
// untranslated key answers the fallback, as say does.
func (w Words) sayLayer(named layerWord) string {
	if w.text == nil {
		return named.text
	}
	if out := w.text(named.key, named.text); out != "" {
		return out
	}
	return named.text
}

// boolean is the one word a screen writes over a value with two answers: the record
// says yes or no, and the page says it in the reader's language.
func (w Words) boolean(v any) string {
	on, ok := v.(bool)
	if !ok || w.text == nil {
		return display.Display(entity.Field{}, v)
	}
	if on {
		return w.text("screens.boolean_true", "Yes")
	}
	return w.text("screens.boolean_false", "No")
}

// number is one measured value in the layout this request's language counts in:
// English writes 12,345 with a comma and Portuguese with a space, and the row carries
// neither. A count and a measurement take two keys because they are written with two
// different verbs — "%g" printed over an integer answers "%!g(int=…)", which a person
// then reads on the page.
func (w Words) number(v any) string {
	if w.phrase == nil {
		return display.Display(entity.Field{}, v)
	}
	switch n := v.(type) {
	case float64:
		return w.phrase("screens.number", "%g", n)
	case float32:
		return w.phrase("screens.number", "%g", float64(n))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return w.phrase("screens.integer", "%d", n)
	default:
		return display.Display(entity.Field{}, v)
	}
}

// moment is one instant under the layout the request's language writes instants in:
// Portuguese puts the day before the month and English does not, so the order is copy
// and not a branch in the renderer.
func (w Words) moment(v any) string {
	if w.text != nil {
		if layout := w.text("screens.datetime", display.DateTimeLayout); layout != display.DateTimeLayout {
			if at, err := time.Parse(time.RFC3339, display.Text(v)); err == nil {
				return at.UTC().Format(layout)
			}
		}
	}
	return display.Display(entity.Field{}, v)
}

// Words is this resource's declared reading in the screen's language: nil
// Locale, the declaration as written — the same rule Options.Text documents.
func (o Options) Words(schema entity.Schema) Words {
	if o.Locale == nil {
		return WordsFor(schema, nil)
	}
	selected := *o.Locale
	return Words{schema: schema, text: func(key, fallback string) string {
		return selected.Formatter.Text(key, fallback)
	}, phrase: func(key, fallback string, args ...any) string {
		return selected.Text(key, fallback, args...)
	}}
}

// words is the scope this Options names for a caller that draws one control rather
// than a whole screen. See Options.Module and Options.Entity.
func (o Options) words() Words {
	return o.Words(entity.Schema{Module: o.Module, Entity: o.Entity})
}
