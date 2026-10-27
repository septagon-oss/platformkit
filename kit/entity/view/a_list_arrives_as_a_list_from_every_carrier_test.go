package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAListArrivesAsAListFromEveryCarrier reads the same list of one element type in
// each carrier a read may hand it — a nil slice, an empty slice, a typed nil of the
// carrier's own slice type, one element, and no key at all — in a record's field, a
// table's cell and a form's control, and asks three things of every one: it is a list,
// it promises the kind a field of that element type arrives as, and a list with nothing
// in it spells `[]` rather than `null`. The element's kind is decided by the declared
// type alone and a field's kind by the switch that reads the field itself, so nothing
// but this comparison keeps the two the same; and a nil slice is the shape a Go handler
// hands for "no elements", which no JSON fixture can carry, so no golden can say which
// way it falls. A shell that reads `elem` and then decodes the elements, or prints a
// count beside `total`, reads a wrong value the moment either answer drifts.
func TestAListArrivesAsAListFromEveryCarrier(t *testing.T) {
	t.Parallel()

	var none []any
	var noStrings []string
	carriers := []struct {
		name   string
		stored any
		empty  bool
	}{
		{name: "a nil slice", stored: none, empty: true},
		{name: "an empty slice", stored: []any{}, empty: true},
		{name: "a typed nil of its own slice type", stored: noStrings, empty: true},
		{name: "one element", stored: []any{json.Number("3")}},
	}
	for _, tc := range carriers {
		s := taskSchema()
		s.Fields = append(s.Fields,
			entity.Field{Name: "single", Type: entity.TypeInt},
			entity.Field{Name: "many", Type: entity.TypeList, Elem: entity.TypeInt})
		in := input(view.KindRecord, []map[string]any{
			{"id": "t1", "title": "n", "single": json.Number("3"), "many": tc.stored},
		}, nil)
		in.Resource.Schema = s
		in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
		in.State.Zone = time.UTC

		cells := recordCells(t, in)
		field, ok := cells["many"]
		if !ok {
			t.Fatalf("a list carrying %s is not on the record, so this case would inspect nothing", tc.name)
		}
		single := cells["single"]
		if field.ValueKind != view.ValueList {
			t.Fatalf("a list carrying %s arrives as %q", tc.name, field.ValueKind)
		}
		if field.Elem != single.ValueKind {
			t.Errorf("a list carrying %s promises %s while a field of that type arrives as %s",
				tc.name, field.Elem, single.ValueKind)
		}
		want := string(bytes(t, field.Value))

		in.State.Kind = view.KindList
		cell := wantCell(t, build(t, in).Table.Rows[0].Cells, "many")
		if got := string(bytes(t, cell.Value)); cell.ValueKind != field.ValueKind || cell.Elem != field.Elem || got != want {
			t.Errorf("a list carrying %s reads list/%s %s in a field and %s/%s %s in a cell",
				tc.name, field.Elem, want, cell.ValueKind, cell.Elem, got)
		}
		if field.Text != cell.Text {
			t.Errorf("a list carrying %s reads %q in a field and %q in a cell", tc.name, field.Text, cell.Text)
		}

		in.State.Kind = view.KindForm
		in.State.Form = view.FormState{Title: "Edit task"}
		control := wantControl(t, build(t, in).Form.Controls, "many")
		if got := string(bytes(t, control.Value)); control.ValueKind != field.ValueKind || control.Elem != field.Elem || got != want {
			t.Errorf("a list carrying %s reads list/%s %s in a field and %s/%s %s in a control",
				tc.name, field.Elem, want, control.ValueKind, control.Elem, got)
		}

		if tc.empty && want != "[]" {
			t.Errorf("a list carrying %s spells %s: a list with nothing in it is an empty list, not a null",
				tc.name, want)
		}
	}

	// A key the row never carried is the field's own absence, which is the one null a
	// list may be: it is not an empty set, and the page says so in the policy's word.
	s := taskSchema()
	s.Fields = append(s.Fields, entity.Field{Name: "many", Type: entity.TypeList, Elem: entity.TypeInt})
	in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "n"}}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	absent, ok := recordCells(t, in)["many"]
	if !ok {
		t.Fatal("a list with no key in the row is not on the record")
	}
	if absent.ValueKind != view.ValueAbsent || string(bytes(t, absent.Value)) != "null" {
		t.Errorf("a list the row never carried reads %s %s, want absent null", absent.ValueKind, bytes(t, absent.Value))
	}
}
