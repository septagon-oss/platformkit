package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// The guidance one field of these cases declares, and the doc one other field carries
// instead of a help line.
const (
	declaredHelp = "One line that says what to write here"
	docHelp      = "Measured against the contract date"
)

// TestAFieldsDeclaredHelpReachesItsControlItsFieldAndItsArgument reads the promise a
// field's guidance makes on its way to a page: whatever the resolver decides that help is,
// every page kind that shows the field shows the same sentence. A person told why a field
// matters on the record must be told it again when the field is asked for on a form and
// when a command takes it as an argument, because that sentence is the only place the page
// explains the field. Both sources the resolver reads are asked for: the help a field
// declares, and the field's own doc where it declares none — the resolver prefers the first
// and falls back to the second, and a page that copied only the declared one would lose the
// guidance for every field that spells it the other way, which is most of them.
func TestAFieldsDeclaredHelpReachesItsControlItsFieldAndItsArgument(t *testing.T) {
	t.Parallel()

	// The record: each field shows its help beside its value, and the command beside it
	// shows the help its own argument declares.
	record := build(t, helpInput(view.KindRecord, nil))
	fields := recordFields(record)
	if got, drawn := fields["notes"]; !drawn {
		t.Fatal("the record drew no notes field, so this case would inspect nothing")
	} else if got.Help != declaredHelp {
		t.Errorf("the record's notes field shows help %q, want the declared %q", got.Help, declaredHelp)
	}
	if got, drawn := fields["slaAt"]; !drawn {
		t.Fatal("the record drew no slaAt field, so this case would inspect nothing")
	} else if got.Help != docHelp {
		t.Errorf("the record's doc-only field shows help %q, want its doc %q", got.Help, docHelp)
	}
	var arguments []view.Control
	for _, a := range record.Actions {
		if a.Kind == view.ActionCommand && a.Verb == "archive" {
			arguments = a.Inputs
		}
	}
	if len(arguments) != 1 || arguments[0].Name != "reason" {
		t.Fatalf("the archive command reached the page with arguments %+v, want the one reason argument this case declared", arguments)
	}
	if arguments[0].Help != declaredHelp {
		t.Errorf("the archive argument shows help %q, want the declared %q", arguments[0].Help, declaredHelp)
	}

	// The form: the same two fields asked for, each with the same sentence under it.
	controls := formControlsByName(build(t, helpInput(view.KindForm, func(s *view.State) {
		s.Form = view.FormState{Title: "Edit task"}
	})))
	if got, drawn := controls["notes"]; !drawn {
		t.Fatal("the form drew no notes control, so this case would inspect nothing")
	} else if got.Help != declaredHelp {
		t.Errorf("the form's notes control shows help %q, want the declared %q", got.Help, declaredHelp)
	}
	if got, drawn := controls["slaAt"]; !drawn {
		t.Fatal("the form drew no slaAt control, so this case would inspect nothing")
	} else if got.Help != docHelp {
		t.Errorf("the form's doc-only control shows help %q, want its doc %q", got.Help, docHelp)
	}
}

// helpInput is one page of kind over a task row whose schema declares help on its title and
// leaves its slaAt field to carry only a doc; commands and policy are rebuilt from that
// schema, so the fields the page draws are the ones this case wrote.
func helpInput(kind view.Kind, state func(*view.State)) view.Input {
	in := input(kind, []map[string]any{{"id": "t1", "title": "Write", "revision": json.Number("3")}}, state)
	s := in.Resource.Schema
	s.Fields = append(s.Fields,
		entity.Field{Name: "notes", Type: entity.TypeString, Presentation: entity.FieldHints{Help: declaredHelp}},
		entity.Field{Name: "slaAt", Type: entity.TypeTime, Doc: docHelp})
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	in.Resource.Commands = []view.CommandDescription{{Verb: "archive", Fields: []entity.Field{
		{Name: "reason", Type: entity.TypeText, Presentation: entity.FieldHints{Help: declaredHelp}},
	}}}
	return in
}

// recordFields are the fields the record's sections show, keyed by field name.
func recordFields(page view.Page) map[string]view.Field {
	out := map[string]view.Field{}
	for _, s := range page.Sections {
		for _, f := range s.Fields {
			out[f.Name] = f
		}
	}
	return out
}

// formControlsByName are the form's controls, keyed by field name.
func formControlsByName(page view.Page) map[string]view.Control {
	out := map[string]view.Control{}
	for _, c := range page.Form.Controls {
		out[c.Name] = c
	}
	return out
}
