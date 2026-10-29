package resource

import (
	"maps"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
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
// differently: the translation, unless a person could read no words in it, in
// which case the declared word. `xtext` refuses a blank or all-space copy when
// it loads, so this guard is against a foreign Formatter, and the cost of not
// having it is copy that reads as nothing wherever a label decides a page.
func (w Words) say(aspect, declared string) string {
	if declared != "" && w.text != nil {
		if out := w.text(w.Key(aspect), declared); strings.TrimSpace(out) != "" {
			return out
		}
	}
	return declared
}

// Entry returns declared entry words in this request's language.
// Mutable blocks are copied before localization; deck queue and directions are
// always copied so callers may safely narrow them to authorized commands.
func (w Words) Entry(p entity.EntryHints) entity.EntryHints {
	out := p
	out.Forms = w.forms(p.Forms)
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
	if p.Saving != nil {
		// The four words of a save control are the entry's own copy, read under the
		// entry's own key, because "Save task" is written by the author who wrote
		// "task" and is translated in the same table. The pair of slugs and the two
		// field names it quotes are identifiers a screen draws and never words.
		saving := *p.Saving
		saving.Words.Save = w.say("saving.save", p.Saving.Words.Save)
		saving.Words.Saved = w.say("saving.saved", p.Saving.Words.Saved)
		saving.Words.Remove = w.say("saving.remove", p.Saving.Words.Remove)
		saving.Words.ListTitle = w.say("saving.listTitle", p.Saving.Words.ListTitle)
		out.Saving = &saving
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
func (w Words) FieldLabel(f entity.Field) string { return w.field(f).Label }

// FieldHelp returns the resolved help for one field.
//
// Deprecated: pass Resource.Resolved to the screen renderer.
func (w Words) FieldHelp(f entity.Field) string { return w.field(f).Help }

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
	return value(w.field(f), map[string]any{f.Name: v}, Options{}).text
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

// Words is this resource's declared reading in the screen's language: nil
// Locale, the declaration as written — the same rule Options.Text documents.
func (o Options) Words(schema entity.Schema) Words {
	if o.Locale == nil {
		return WordsFor(schema, nil)
	}
	selected := *o.Locale
	return WordsFor(schema, func(key, fallback string) string {
		return selected.Formatter.Text(key, fallback)
	})
}
