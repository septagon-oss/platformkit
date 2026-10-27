package resource

// command controls are native POST forms whose copy and flags come from policy.

import (
	"encoding/hex"
	"strconv"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Command supplies an authorized route and its argument bindings, without
// execution closures. Resolved supplies presentation; Present is the legacy input.
type Command struct {
	// Resolved supplies command copy and flags after authority has been narrowed.
	Resolved *present.ResolvedCommand
	// ScreenOwner names the module whose screens these anchors live on; Repeated marks
	// a view whose controls no single form can be pointed at.
	ScreenOwner, Entity string
	Repeated            bool
	// Bindings fills hidden arguments from the displayed row, never a fresh read.
	Bindings    map[string]string
	Verb        string
	Label       string
	Description string
	Fields      []entity.Field
	Collection  bool
	// Base is the path the command's routes are mounted *under*: the list path for
	// a collection command, and the item path for a row command — which only the
	// renderer knows once it has the row, so Action appends the verb to it.
	Base string
	// Present supplies declarations when no supported resource policy is supplied.
	Present entity.CommandHints
}

// Action appends the verb to Base for a collection command or item for a
// record command. It preserves the adapter's route and never resolves authority.
func (c Command) Action(item string) string {
	if c.Collection {
		return c.Base + "/" + c.Verb
	}
	return item + "/" + c.Verb
}

// CommandForms renders the authorized policy commands belonging to this surface.
// item is the record address; collection forms use each command's supplied Base.
func CommandForms(r Resource, o Options, item string, collection bool, row map[string]any) []g.Node {
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return []g.Node{invalid(err)}
	}
	return commandFormsIn(o, o.Words(r.Schema), r.Commands, item, collection, nil, row)
}

// commandFormsIn expands revision-map commands once per supplied locale.
// Without locale states, each authorized command has one form.
func commandFormsIn(o Options, words Words, commands []Command, item string, collection bool,
	locales []entity.LocaleState, row map[string]any) []g.Node {
	out := make([]g.Node, 0, len(commands))
	primaryUsed := false
	form := func(c Command, locale entity.LocaleState) g.Node {
		if c.Resolved.Primary {
			policy := *c.Resolved
			policy.Primary, primaryUsed = !primaryUsed, true
			c.Resolved = &policy
		}
		return commandFormFor(o, words, c, item, locale, row)
	}
	for _, c := range commands {
		if c.Collection != collection {
			continue
		}
		perLocale := carriesRevisions(c)
		if perLocale && len(locales) > 0 {
			for _, l := range locales {
				out = append(out, form(c, l))
			}
			continue
		}
		out = append(out, form(c, entity.LocaleState{}))
	}
	return out
}

// carriesRevisions reports whether one of this command's arguments is a map whose
// keys the screen fills in on the person's behalf — a per-field revision, which
// only a read of this row in this language can supply. That is the whole test: it
// is a fact the command declared about its own arguments, and not a verb this
// package has learned to recognise.
func carriesRevisions(c Command) bool { return present.RepeatedFields(c.Fields) }

// commandFormFor renders policy copy and arguments at the command's route.
// Locale and revision bindings use only values already read for this page.
func commandFormFor(o Options, words Words, c Command, item string, locale entity.LocaleState, row map[string]any) g.Node {
	policy := c.Resolved

	label, button := policy.Label, policy.Label
	warning := policy.Confirmation
	if warning != nil {
		if warning.Title != "" {
			label = warning.Title
		}
		if warning.ConfirmLabel != "" {
			button = warning.ConfirmLabel
		}
	}
	controls := make([]g.Node, 0, len(c.Fields)+2)
	if c.Description != "" {
		controls = append(controls, components.FormSentence(components.FormSentenceProps{Text: c.Description}))
	}
	if warning != nil && warning.Body != "" {
		// The consequence is the form's own copy, so the design system owns its text
		// scale and its reading measure. Drawn as a bare paragraph it arrived on a
		// generated record page at 16px across 142ch — a third body size beside the
		// help line and the label, and a measure the design floor refuses.
		controls = append(controls, components.FormSentence(components.FormSentenceProps{Text: warning.Body}))
	}
	if locale.Locale != "" {
		// The form is for this language, so the language is carried and not asked
		// for, and the sentence beside the button says which one the person is
		// about to write.
		label = label + " — " + locale.Locale
	}
	arguments := words.forCommand(c.Verb)
	// Hex keeps action, locale and field boundaries distinct, including punctuation
	// and empty locales, without consuming the components' help/error suffixes.
	scope := "pk-command-" + hex.EncodeToString([]byte(c.Action(item))) + "-" + hex.EncodeToString([]byte(locale.Locale))
	for _, f := range c.Fields {
		id := scope + "-field-" + hex.EncodeToString([]byte(f.Name))
		if source := c.Bindings[f.Name]; source != "" {
			// The argument comes from the row the person is looking at, so the box is
			// hidden and still carries this control's own identity.
			hidden := f
			hidden.Widget, hidden.Present, hidden.Choices, hidden.Presentation.Figure = "hidden", "", nil, nil
			controls = append(controls, forms.Control(forms.ControlProps{
				Field: formField(arguments, hidden, o), ID: id, Value: display.Text(row[source]),
			}))
			continue
		}
		if len(f.Keys) > 0 {
			for _, k := range f.Keys {
				controls = append(controls, mapKeyControl(id+"-key-"+hex.EncodeToString([]byte(k.Name)), f, k, locale, c.fieldAnchor(f.Name, k.Name)))
			}
			continue
		}
		value := ""
		if locale.Locale != "" && f.Name == "lang" {
			f.Widget, value = "hidden", locale.Locale
		}
		controls = append(controls, forms.Control(forms.ControlProps{
			Field: c.formField(arguments, f, o), ID: id, Value: value, ChoiceValues: choiceDefault(f),
		}))
	}
	variant, tone := "outline", ""
	if policy.Primary {
		variant = ""
	}
	if policy.Destructive {
		tone = "danger"
	}
	controls = append(controls, components.Button(components.ButtonProps{
		ComponentProps: components.ComponentProps{Attrs: c.submitAttrs()},
		Label:          button, Type: "submit", Variant: variant, Tone: tone, Size: "md",
	}))
	return components.Form(components.FormProps{ComponentProps: components.ComponentProps{Attrs: c.viewAttrs()}, Action: c.Action(item), Label: label}, controls...)
}

// mapKeyControl renders one named map argument. Revision keys quote the
// supplied locale's observed revision; other keys begin empty to avoid rewriting
// values the person did not submit. An anchor names the key in the published view.
func mapKeyControl(id string, f entity.Field, k entity.MapKey, locale entity.LocaleState, anchor screen.AnchorRef) g.Node {
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
	return forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: key, Label: k.Label, Anchor: anchor},
		ID:    id, Value: value,
	})
}
