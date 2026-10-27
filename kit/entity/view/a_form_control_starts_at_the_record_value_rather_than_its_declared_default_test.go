package view_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFormControlStartsAtTheRecordValueRatherThanItsDeclaredDefault reads the rule every
// control on a form shares: the row's own value where the row holds one, the schema's
// declared default where it holds none — a stored null included, because a null is a field
// with nothing in it, not a field holding nothing — and nothing at all where neither has a
// value. A form that starts a record at the declaration instead of at the record invites the
// person to write the default back over what is stored, and one that turns an absent value
// into an empty one asks them to confirm a value no command would accept. The control still
// advertises the declared default, which is what a shell re-offers when the person clears the
// field. Money is left to its own case: this one covers the kinds that share the leg.
func TestAFormControlStartsAtTheRecordValueRatherThanItsDeclaredDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
		start any
	}{
		{name: "the row's own value wins over the declared default", value: "done", start: "done"},
		{name: "an explicit null takes the declared default", value: nil, start: "open"},
	} {
		row := map[string]any{"id": "t1", "title": "n", "revision": json.Number("3"), "status": tc.value}
		control := formControl(t, "status", row)
		if control.ValueKind != view.ValueString || control.Value != tc.start {
			t.Errorf("%s: the status control starts as %s %v, want string %v",
				tc.name, control.ValueKind, control.Value, tc.start)
		}
		if control.Default != "open" {
			t.Errorf("%s: the control advertises default %q, want the declared open, which a shell re-offers",
				tc.name, control.Default)
		}
	}

	// One row, one value per kind, each field declaring a default the row contradicts: the
	// row must win on every kind at once, so a leg that prefers the declaration for one type
	// cannot hide behind the others carrying it.
	controls := formControls(t, map[string]any{
		"id": "t1", "revision": json.Number("3"), "done": false,
		"count": json.Number("7"), "dueAt": "2026-07-01T14:12:00Z",
	})
	for _, tc := range []struct {
		name  string
		kind  view.ValueKind
		start any
	}{
		{name: "done", kind: view.ValueBoolean, start: false},
		{name: "count", kind: view.ValueInteger, start: int64(7)},
		{name: "dueAt", kind: view.ValueInstant, start: "2026-07-01T14:12:00.000000Z"},
	} {
		c := controls[tc.name]
		if c.ValueKind != tc.kind || c.Value != tc.start {
			t.Errorf("%s starts as %s %v, want %s %v: the record's own value, not the declared default",
				tc.name, c.ValueKind, c.Value, tc.kind, tc.start)
		}
	}
	if labels := controls["labels"]; labels.ValueKind != view.ValueAbsent || labels.Value != nil {
		t.Errorf("a field the row leaves out and the schema declares no default for starts as %s %v, want absent with a null value",
			labels.ValueKind, labels.Value)
	}
	if day, isText := controls["dueAt"].Value.(string); !isText || !strings.HasPrefix(day, "2026-07-01") {
		t.Errorf("the due control starts at %v, which does not carry the day the row holds", controls["dueAt"].Value)
	}
}

// formControls are the update form's controls over row: the task schema widened with one
// plain number, and status, that number, the deadline and the labels each declaring a default
// a row of this package's own values contradicts, except the labels, which declares none.
func formControls(t *testing.T, row map[string]any) map[string]view.Control {
	t.Helper()
	s := taskSchema()
	s.Fields = append(s.Fields, entity.Field{Name: "count", Type: entity.TypeInt})
	for i := range s.Fields {
		if def, declared := defaultOf(s.Fields[i].Name); declared {
			s.Fields[i].Default = def
		}
	}
	in := input(view.KindForm, []map[string]any{row}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	in.State.Form = view.FormState{Title: "Edit task"}
	in.State.Zone = time.UTC
	out := map[string]view.Control{}
	for _, c := range build(t, in).Form.Controls {
		out[c.Name] = c
	}
	if len(out) == 0 {
		t.Fatal("the form shows no controls, so this case would inspect nothing")
	}
	return out
}

// formControl is the named control of that form, and fails when the form hides the field.
func formControl(t *testing.T, name string, row map[string]any) view.Control {
	t.Helper()
	c, shown := formControls(t, row)[name]
	if !shown {
		t.Fatalf("the form shows no %q control, so this case would inspect nothing", name)
	}
	return c
}

// defaultOf is the default each named field declares: whole text, a whole number and one
// instant, none of them the value the row of these cases carries.
func defaultOf(name string) (string, bool) {
	def, ok := map[string]string{
		"status": "open", "count": "1", "dueAt": "2026-01-02T03:04:05Z",
	}[name]
	return def, ok
}
