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
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
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
func commandForms(o Options, words Words, commands []Command, item string, collection bool) []g.Node {
	out := make([]g.Node, 0, len(commands))
	for _, c := range commands {
		if c.Collection != collection {
			continue
		}
		out = append(out, commandForm(o, words, c, item))
	}
	return out
}

// commandForm is the control. One control per argument, through the same Control the
// edit form uses, so a command that takes an assignee gets the same picker a field of
// that type gets, and a command that takes none is a button and not a guess about what
// it might want.
func commandForm(o Options, words Words, c Command, item string) g.Node {
	// The form's accessible name is the sentence the module wrote for the API, and
	// the button is the short verb phrase: a page with three command forms on it has
	// to tell a reader what each one is for, and three buttons reading "Assign",
	// "Resolve", "Archive" with no prose is a guess about consequence.
	label := words.CommandLabel(c.Verb, c.Present)
	if label == "" {
		// Nothing was declared, so there is nothing to translate: the screen came
		// to this renderer already wearing the route's Summary as its name.
		label = c.Label
	}
	if c.Description != "" {
		label = c.Description
	}
	warning := words.Command(c.Verb, c.Present).Confirmation
	if warning != nil && warning.Title != "" {
		// The author wrote the consequence, so the form asks in those words and
		// says what it costs underneath. Without them nothing is invented here —
		// see this file's own header.
		label = warning.Title
	}
	button := c.Label
	if declared := words.CommandLabel(c.Verb, c.Present); declared != "" {
		button = declared
	}
	if warning != nil && warning.ConfirmLabel != "" {
		button = warning.ConfirmLabel
	}
	controls := make([]g.Node, 0, len(c.Fields)+2)
	if warning != nil && warning.Body != "" {
		controls = append(controls, h.P(g.Text(warning.Body)))
	}
	arguments := words.forCommand(c.Verb)
	for _, f := range c.Fields {
		controls = append(controls, controlWords(arguments, f, "", "", false))
	}
	controls = append(controls, components.Button(components.ButtonProps{
		Label: button, Type: "submit", Size: "md",
	}))
	return components.Form(components.FormProps{Action: c.Action(item), Label: label}, controls...)
}
