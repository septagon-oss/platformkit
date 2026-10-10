package resource

// commands.go is the read side of a lifecycle route. The five CRUD operations have
// always had generated controls because they are the same at every entity; a
// command is what is different about an entity — assign, resolve, claim a handle,
// report a problem — and until now each one arrived at the screen as nothing at all,
// so a person looking at a task they were allowed to resolve had Edit and Delete on
// offer and no way to resolve it.
//
// A command control is a real POST form, so it works with JavaScript switched off,
// exactly as the delete form does. It is not a confirmation dialog: Delete asks you to
// confirm because it destroys, and a command is not inherently destructive —
// "acknowledge" and "recheck the SLA" are commands too — so this package does not
// invent a warning about an action whose consequence it cannot know. What it does
// instead is print the consequence its author wrote: a command that declares a
// `confirmation` asks in that title, says that body above the button and labels the
// button with that confirmLabel. An undeclared one stays exactly the form it was.
// The Summary and Description the module wrote for the API are the rest of what the
// person on the screen reads.

import (
	"cmp"
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Command is one lifecycle route as a screen needs it: what to call it, what it does,
// what it asks for, whether it acts on the whole list or on one row, and where its
// route was mounted.
//
// There is deliberately no closure here. ui/resource draws and does not write — that
// is docs/adr/0007 — and the guard that decides whether this caller may use the
// command has already been applied by the adapter, which lists only what
// httpx.Resource.CommandsFor answered with. A command a caller may not use is not
// rendered dimmed or disabled: the door is not there, because the route behind it is
// not reachable by them either.
type Command struct {
	Verb        string
	Label       string
	Description string
	Fields      []entity.Field
	Collection  bool
	// Base is the path the command's routes are mounted *under*: the list path for
	// a collection command, and the item path for a row command — which only the
	// renderer knows once it has the row, so Action appends the verb to it.
	Base string
	// Present is how the command's author described it. This renderer draws the
	// confirmation from it when one is declared, and still invents none when it
	// is not: a warning about an action whose consequence nobody wrote down would
	// be a guess with a button on it. Primary and Destructive travel with the
	// command and are not drawn here — the web button tone is a design decision
	// of its own.
	Present entity.CommandHints
}

// Action is where this command's form posts, and both shapes end in the verb,
// because the verb is the route. It is worth being explicit about the collection
// case: the list path is where a *create* is posted, so a collection command whose
// action stopped at Base would press "Reindex everything" and create a row. The
// assertion that found this is TestEveryCommandTheCallerMayUseHasExactlyOneControl.
func (c Command) Action(item string) string {
	if c.Collection {
		return c.Base + "/" + c.Verb
	}
	return item + "/" + c.Verb
}

// commandForms is one form per command that belongs on this screen: the ones that
// act on a whole list are not offered beside a single row, and a command that acts
// on a row has no meaning above a table of many.
func commandForms(o Options, commands []Command, item string, collection bool) []g.Node {
	return commandFormsIn(o, commands, item, collection, nil)
}

// commandFormsIn is the same with one row's standing in each language the tenant
// speaks besides its own. A command that asks for one text per field of the record
// asks per language, and the language is not something the person is left to type
// into a box beside it: the screen knows which languages this record is owed, and
// the same command offered once per language is one form per language, each with
// the revisions it read already in it. A screen with no languages to offer — the
// design export, a monolingual tenant, the list screen — offers the command as it
// always has, once.
func commandFormsIn(o Options, commands []Command, item string, collection bool,
	locales []entity.LocaleState) []g.Node {
	out := make([]g.Node, 0, len(commands))
	for _, c := range commands {
		if c.Collection != collection {
			continue
		}
		perLocale := carriesRevisions(c)
		if perLocale && len(locales) > 0 {
			for _, l := range locales {
				out = append(out, commandFormFor(o, c, item, l))
			}
			continue
		}
		out = append(out, commandFormFor(o, c, item, entity.LocaleState{}))
	}
	return out
}

// carriesRevisions reports whether one of this command's arguments is a map whose
// keys the screen fills in on the person's behalf — a per-field revision, which
// only a read of this row in this language can supply. That is the whole test: it
// is a fact the command declared about its own arguments, and not a verb this
// package has learned to recognise.
func carriesRevisions(c Command) bool {
	for _, f := range c.Fields {
		for _, k := range f.Keys {
			if k.From == entity.KeyFromRevision {
				return true
			}
		}
	}
	return false
}

// commandFormFor is the control, and one row's standing in one language: one control
// per argument, through the same Control the edit form uses, so a command that takes
// an assignee gets the same picker a field of that type gets, and a command that takes
// none is a button and not a guess about what it might want. The language is a control
// like any other until the screen knows which one this form is for; then it is carried
// and the sentence beside the button names it.
func commandFormFor(o Options, c Command, item string, locale entity.LocaleState) g.Node {
	// The form's accessible name is the sentence the module wrote for the API, and
	// the button is the short verb phrase: a page with three command forms on it has
	// to tell a reader what each one is for, and three buttons reading "Assign",
	// "Resolve", "Archive" with no prose is a guess about consequence.
	label := c.Label
	if c.Description != "" {
		label = c.Description
	}
	warning := c.Present.Confirmation
	if warning != nil && warning.Title != "" {
		// The author wrote the consequence, so the form asks in those words and
		// says what it costs underneath. Without them nothing is invented here —
		// see this file's own header.
		label = warning.Title
	}
	button := c.Label
	if warning != nil && warning.ConfirmLabel != "" {
		button = warning.ConfirmLabel
	}
	controls := make([]g.Node, 0, len(c.Fields)+2)
	if warning != nil && warning.Body != "" {
		controls = append(controls, h.P(g.Text(warning.Body)))
	}
	if locale.Locale != "" {
		// The form is for this language, so the language is carried and not asked
		// for, and the sentence beside the button says which one the person is
		// about to write.
		label = label + " — " + locale.Locale
	}
	for _, f := range c.Fields {
		if len(f.Keys) > 0 {
			for _, k := range f.Keys {
				controls = append(controls, mapKeyControl(f, k, locale))
			}
			continue
		}
		if locale.Locale != "" && f.Name == "lang" {
			hidden := f
			hidden.Widget = "hidden"
			controls = append(controls, Control(hidden, locale.Locale, "", false))
			continue
		}
		controls = append(controls, Control(f, "", "", false))
	}
	controls = append(controls, components.Button(components.ButtonProps{
		Label: button, Type: "submit", Size: "md",
	}))
	return components.Form(components.FormProps{Action: c.Action(item), Label: label}, controls...)
}

// mapKeyControl is one key of a map-valued argument: a control of the key's own
// widget, named the way a form names an entry of a map — `values[title]` — and
// filled with what the screen already knows about that field of this row when the
// key says it is filled for the person rather than by them.
//
// The value is the revision, not the stored text. A screen that prefilled the box
// with the current translation would be a screen that invites a person to retype
// what they did not mean to change, and the command writes every field the body
// names: an unchanged box reposted without its revision would be a 409 on six
// fields of which one was edited. An empty box is a field left alone.
func mapKeyControl(f entity.Field, k entity.MapKey, locale entity.LocaleState) g.Node {
	key := entity.Field{
		Name:   f.Name + "[" + k.Name + "]",
		Type:   f.Elem,
		Widget: k.Widget,
		Doc:    f.Doc,
	}
	value := ""
	if k.From == entity.KeyFromRevision {
		value = strconv.FormatInt(locale.Revisions[k.Name], 10)
	}
	// The control's identity is per form and per language: one record screen
	// offers one translate form per language the tenant speaks, and six inputs
	// sharing three ids is a label that points at somebody else's box.
	id := "pk-translate-" + cmp.Or(locale.Locale, "any") + "-" + key.Name
	return forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: key, Label: k.Label},
		ID:    id, Value: value,
	})
}
