package resource

import (
	"maps"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

// hints.go is the one place a *declared* reading string — a field label, an
// entry's singular, a command's confirmation — is put through the copy seam.
//
// The rule behind the whole file is that an author writes words once, in their
// own English, in the declaration itself: `Present:` on a Spec, a `ui:"…"` tag on
// a field. No second table of keys sits beside it, because a key spelled beside a
// string that is already written is a second copy of that string and a typo that
// nothing checks. So the key is *derived* from the declaration's own address:
//
//	hints.<module>/<entity>.<aspect>
//
// where `<module>/<entity>` is the resource address `rest.resourceAddress` keys
// the registry by (and `reference:` spells a target), and `<aspect>` names the
// declaration: `singular`, `plural`, `description`, `emptyDescription`,
// `group.<key>`, `section.<key>`, `field.<name>.label`, `field.<name>.help`,
// `enum.<field>.<value>`, `command.<verb>.label`,
// `command.<verb>.confirmation.<title|body|confirmLabel>`,
// `command.<verb>.successMessage`, and a command argument's
// `command.<verb>.field.<name>.label` / `.help`.
//
// What is *not* resolved is the rest of the vocabulary, and the reason is one
// sentence: `icon`, `group.key`, `section.key`, `order`, `primaryField`,
// `previewField`, `statusField`, `summaryFields`, `sortable`, `visibility`,
// `format`, `money`, `reference`, `enumTones`, `system`, `primary`,
// `destructive` and `verb` are names a screen draws, not words a person reads —
// translating `warning` or `detail` would be a bug, not a localisation.
//
// The declared literal is always the fallback, which is why no `en.json` exists
// anywhere: the sentence the code author wrote is the English (decision 0012 rule
// 2, the same rule `ui/page`'s fault copy and every module's permission label
// follow). A translation is therefore only ever written for the second language,
// and `xtext`'s own parity gate is what makes writing half of one a refusal at
// composition rather than a screen drawn in two languages.

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

// Entry is this resource's entry declaration in the request's language. It
// copies what it changes — the entry hints ride on the registration, shared by
// every request — and returns the same value untouched when nothing is
// translated.
func (w Words) Entry(p entity.EntryHints) entity.EntryHints {
	if w.text == nil {
		return p
	}
	out := entity.EntryHints{
		Singular: w.say("singular", p.Singular), Plural: w.say("plural", p.Plural),
		Description: w.say("description", p.Description), Icon: p.Icon,
		Order: p.Order, PrimaryField: p.PrimaryField, PreviewField: p.PreviewField,
		SummaryFields: p.SummaryFields, StatusField: p.StatusField,
		EmptyDescription: w.say("emptyDescription", p.EmptyDescription),
		Sortable:         p.Sortable,
	}
	if p.Group != nil {
		group := *p.Group
		group.Label = w.say("group."+group.Key, group.Label)
		out.Group = &group
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

// FieldLabel is what a control, a header and a term call this field, in this
// request's language: the declared label translated, and the words
// display.FieldLabel computes (Humanize of the name) left alone, because nobody
// wrote a string here to translate.
func (w Words) FieldLabel(f entity.Field) string {
	if f.Presentation.Label == "" {
		return display.FieldLabel(f)
	}
	return w.say("field."+f.Name+".label", f.Presentation.Label)
}

// FieldHelp is the line under a control, in this request's language. An
// undeclared help line is the field's own Doc, which is a developer sentence no
// copy table holds until its author declares a `help:` of their own.
func (w Words) FieldHelp(f entity.Field) string {
	if f.Presentation.Help == "" {
		return display.FieldHelp(f)
	}
	return w.say("field."+f.Name+".help", f.Presentation.Help)
}

// EnumWord is one enum value as its field's author named it, in this request's
// language. The tone of that value is not a word and is never translated.
func (w Words) EnumWord(f entity.Field, value string) string {
	word := f.Presentation.EnumLabels[value]
	if word == "" {
		return display.EnumWord(f, value)
	}
	return w.say("enum."+f.Name+"."+value, word)
}

// Value is a field's value as a screen shows it, with this request's enum
// words: display.Display in every respect except the one a declared
// `enumLabels` map reaches.
func (w Words) Value(f entity.Field, v any) string {
	if w.text == nil || len(f.Presentation.EnumLabels) == 0 {
		return display.Display(f, v)
	}
	return display.DisplayWord(f, v, w.EnumWord)
}

// CommandLabel is the word on this command's button, in this request's
// language; undeclared, the route's own Summary, which is what ui/screens
// reads today because nothing else existed.
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
