package resource

import (
	"maps"
	"time"

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
// follow). A translation is therefore only ever written for the second language.
// What refuses a declaration shipped without that half is not the loader: an
// `xtext` parity gate compares the locales one source file carries, and a module
// that ships one translated file has no second locale to be compared with. Here
// the completeness check belongs to the composition that reads the table —
// `TestEveryDeclaredHintStringIsAnsweredInPortuguese` in `apps/platformkit`
// resolves every key this scheme can derive against `pt-PT` — so a missing answer
// fails that test rather than arriving as a screen drawn in two languages.

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
	// phrase asks for the layout a value is written in — a count, a measurement,
	// an instant. It is a second seam beside text because those strings take an
	// argument and a label does not; kit/locale's Formatter answers both shapes, so
	// one language serves both and this type keeps the two calls apart. Nil with no
	// catalogue: a value prints the digits it carries.
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

// Entry is this resource's entry declaration in the request's language, and
// returns the same value untouched when nothing is translated.
//
// It starts from the declared value and overwrites the four words and the two
// label-carrying blocks, because which parts of a reading are words is the
// translation scheme's business and not this function's: a rebuild that lists
// fields has to be told, every time, that the next hint a shell reads is not a
// word, and a hint nobody told it about reaches a served document in one language
// only. `entity.EntryHints` carries no unexported field and no pointer a caller
// mutates, so the copy is the whole declaration.
//
// The entry hints ride on the registration, shared by every request, so the two
// values whose labels change — the group, each section — are copied out of the
// shared value before they are written. Every slice stays shared: nothing here
// writes one.
func (w Words) Entry(p entity.EntryHints) entity.EntryHints {
	if w.text == nil {
		return p
	}
	out := p
	out.Singular = w.say("singular", p.Singular)
	out.Plural = w.say("plural", p.Plural)
	out.Description = w.say("description", p.Description)
	out.EmptyDescription = w.say("emptyDescription", p.EmptyDescription)
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

// Value is a field's value as a screen shows it, in this request's language: the
// declared reading of an enum under its `enumLabels` map, and the layout a boolean,
// a measured number or an instant is written in — the words and the layouts a
// catalogue holds, which is why a Portuguese row reads "12 345" where an English one
// reads "12,345". Everything else is display.Display: a value no language writes
// differently is that value, and an absent value is the dash every other empty cell
// on this screen says.
func (w Words) Value(f entity.Field, v any) string {
	if v != nil {
		switch f.Type {
		case entity.TypeTime:
			return w.moment(f, v)
		case entity.TypeBool:
			return w.boolean(f, v)
		case entity.TypeInt, entity.TypeFloat:
			return w.number(f, v)
		}
	}
	if w.text == nil || len(f.Presentation.EnumLabels) == 0 {
		return display.Display(f, v)
	}
	return display.DisplayWord(f, v, w.EnumWord)
}

// boolean is the one word a screen writes over a value that has two answers. The
// record says yes or no; the page says it in the reader's language, which is the
// difference between data and copy — and "Yes" printed under pt-PT is copy that
// stopped being true when the page changed language.
func (w Words) boolean(f entity.Field, v any) string {
	on, ok := v.(bool)
	if !ok || w.text == nil {
		return display.Display(f, v)
	}
	if on {
		return w.text("screens.boolean_true", "Yes")
	}
	return w.text("screens.boolean_false", "No")
}

// number is one measured value in the layout this request's language counts in:
// English writes 12,345 with a comma and Portuguese with a space, and the row carries
// neither. A count and a measurement take two keys because they are written with two
// different verbs — "%g" prints a float and answers an integer with "%!g(int=…)",
// which a person then reads on the page — and a language that groups both alike says
// so twice in its copy rather than having this file decide its punctuation.
func (w Words) number(f entity.Field, v any) string {
	if w.phrase == nil {
		return display.Display(f, v)
	}
	switch n := v.(type) {
	case float64:
		return w.phrase("screens.number", "%g", n)
	case float32:
		return w.phrase("screens.number", "%g", float64(n))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return w.phrase("screens.integer", "%d", n)
	default:
		return display.Display(f, v)
	}
}

// moment is one instant under the layout the request's language writes instants in:
// Portuguese puts the day before the month and English does not, so the order is copy
// and not a branch in the renderer. A value that is not an instant, and a render with
// no catalogue, are what kit/entity/display has always printed.
func (w Words) moment(f entity.Field, v any) string {
	if w.text != nil {
		if layout := w.text("screens.datetime", display.DateTimeLayout); layout != display.DateTimeLayout {
			if at, err := time.Parse(time.RFC3339, display.Text(v)); err == nil {
				return at.UTC().Format(layout)
			}
		}
	}
	return display.Display(f, v)
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

// words is the scope this Options names for itself, for a caller that draws one
// control outside a whole screen. See Options.Module and Options.Entity.
func (o Options) words() Words {
	return o.Words(entity.Schema{Module: o.Module, Entity: o.Entity})
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
