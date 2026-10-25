package view

import (
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// actions are the doors this page opens, in the order the generated screens draw
// them: create, update, delete, then the commands in policy order. A form page
// carries none — its submit is the form's own act, stated by Form.Create and
// Form.ExpectedRevision — and a page for a caller with no write grant carries no CRUD
// door, because drawing a door the caller cannot walk through is a lie about the
// caller, not about the resource. A page grants nothing and checks nothing: it draws
// or does not draw, and the command rechecks everything inside its own transaction.
//
// A command reaches an action only from both halves of its declaration: the policy's
// words, visibility and tone, and the description's arguments, scope and undo. One
// half alone loses the action, because a button that posts a body nobody declared is a
// lie with a label on it.
func (b builder) actions(row map[string]any) []Action {
	if b.in.State.Kind == KindForm {
		return nil
	}
	target := Ref{Resource: b.in.Resource.Resource}
	out := []Action{}
	if b.in.State.Kind == KindList && b.door("create") {
		out = append(out, Action{Kind: ActionCreate, Verb: "create", Scope: ScopeCollection, Target: target,
			Inputs: []Control{}})
	}
	if b.in.State.Kind == KindRecord && row != nil {
		target.ID = idOf(row)
		if b.door("update") {
			out = append(out, Action{Kind: ActionUpdate, Verb: "update", Scope: ScopeRecord, Target: target,
				Inputs: []Control{}})
		}
		// A delete costs something by the meaning of the verb, so it marks itself
		// destructive; the words of its confirmation stay the shell's.
		if b.door("delete") {
			out = append(out, Action{Kind: ActionDelete, Verb: "delete", Scope: ScopeRecord, Target: target,
				Destructive: true, Inputs: []Control{}})
		}
	}
	return append(out, b.commands(row)...)
}

// commands are the policy's commands that the resource also describes, in policy
// order, narrowed to the scope this page is on: a list offers the commands that act on
// the collection, a record the ones that act on it.
func (b builder) commands(row map[string]any) []Action {
	out := []Action{}
	for _, rc := range b.in.Policy.Commands {
		d, ok := b.described(rc.Verb)
		if rc.System || !rc.Visible || !ok || d.Collection != (b.in.State.Kind == KindList) {
			continue
		}
		action := Action{
			Kind: ActionCommand, Verb: rc.Verb, Label: rc.Label, Undo: d.Undo, HasResult: d.HasResult,
			Primary: rc.Primary, Destructive: rc.Destructive, Inputs: namesOrNil(b.inputs(d)),
			Target: b.ref(b.in.State.Kind, row),
		}
		if d.Collection {
			action.Scope = ScopeCollection
		} else {
			action.Scope = ScopeRecord
		}
		if rc.Confirmation != nil {
			action.Confirm = Confirm{Title: rc.Confirmation.Title, Body: rc.Confirmation.Body,
				ConfirmLabel: rc.Confirmation.ConfirmLabel}
		}
		out = append(out, action)
	}
	return out
}

// described finds the resource's own half of a command declaration.
func (b builder) described(verb string) (CommandDescription, bool) {
	for _, d := range b.in.Resource.Commands {
		if d.Verb == verb {
			return d, true
		}
	}
	return CommandDescription{}, false
}

// door reports whether this page draws one CRUD verb: the resource answers it and the
// caller holds the write grant.
func (b builder) door(verb string) bool {
	return b.in.State.Writable && b.in.Resource.offers(verb)
}

// inputs are one command's arguments: the schema's facts about each one, read through
// the same resolver the fields go through, so that a control, an option list and a
// value spelling mean one thing in both places.
func (b builder) inputs(d CommandDescription) []Control {
	resolved := present.Resolve(present.Input{
		Schema:   entity.Schema{Module: moduleOf(b.in.Resource.Resource), Entity: d.Verb, Fields: d.Fields},
		Language: b.in.Policy.Language,
	})
	out := make([]Control, 0, len(d.Fields))
	for _, f := range d.Fields {
		rf := resolvedField(resolved, f.Name)
		kind, elem, value := b.value(rf, f, startOf(nil, f.Name, f.Default))
		out = append(out, Control{Name: f.Name, Label: rf.Label, Help: rf.Help, Control: rf.Control,
			Required: f.Required, MaxLength: f.MaxLength, Default: f.Default,
			ValueKind: kind, Elem: elem, Value: value,
			Options: namesOrNil(rf.Options), Keys: namesOrNil(f.Keys)})
	}
	return out
}

// resolvedField is the policy's reading of one argument, or the plain reading of a
// field the resolver did not name.
func resolvedField(resolved present.Resolved, name string) present.ResolvedField {
	for _, rf := range resolved.Fields {
		if rf.Name == name {
			return rf
		}
	}
	return present.ResolvedField{Name: name}
}

// moduleOf is the first half of a resource's `module/entity` name, "" for a page
// nobody registered.
func moduleOf(resource string) string {
	module, _, _ := strings.Cut(resource, "/")
	return module
}
