package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAListMeansTheSameInAFieldACellAndAControl reads one list of each element type a
// schema may declare in the three places a Page shows a value — a record's field, a
// table's cell and a form's control — and asks that the value kind, the element kind and
// the elements' JSON spelling are one thing in all three, and the read text one in the
// two places that print it. Three passes of one builder draw them, one shell paints all
// three on one screen, and the control is where an edit starts from the row's own value:
// a list arriving there in another shape is a value the write changes before anyone
// types it.
func TestAListMeansTheSameInAFieldACellAndAControl(t *testing.T) {
	t.Parallel()

	admitted := []struct {
		elem   entity.FieldType
		stored any
	}{
		{entity.TypeString, "a"}, {entity.TypeText, "long text"},
		{entity.TypeUUID, "9f1b1f0c-2f6a-4d6d-9f2f-6f1a2b3c4d5e"}, {entity.TypeBool, true},
		{entity.TypeInt, json.Number("3")}, {entity.TypeFloat, json.Number("1.50")},
		{entity.TypeTime, "2026-07-01T14:12:00.000000Z"},
	}
	for _, tc := range admitted {
		s := taskSchema()
		s.Fields = append(s.Fields, entity.Field{Name: "tags", Type: entity.TypeList, Elem: tc.elem})
		in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "n", "tags": []any{tc.stored}}}, nil)
		in.Resource.Schema = s
		in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)

		f := recordCells(t, in)["tags"]
		want := spell(t, f.ValueKind, f.Elem, f.Value)

		in.State.Kind = view.KindList
		row := build(t, in).Table.Rows
		if len(row) == 0 {
			t.Fatal("the list shows no row, so this case would inspect nothing")
		}
		c := wantCell(t, row[0].Cells, "tags")
		gotCell := spell(t, c.ValueKind, c.Elem, c.Value)
		if gotCell != want {
			t.Errorf("a list of %s reads %s in a field and %s in a cell", tc.elem, want, gotCell)
		}
		if f.Text != c.Text {
			t.Errorf("a list of %s reads %q in a field and %q in a cell", tc.elem, f.Text, c.Text)
		}

		in.State.Kind = view.KindForm
		in.State.Form = view.FormState{Title: "Edit task"}
		k := wantControl(t, build(t, in).Form.Controls, "tags")
		gotControl := spell(t, k.ValueKind, k.Elem, k.Value)
		if gotControl != want {
			t.Errorf("a list of %s reads %s in a field and %s in a control", tc.elem, want, gotControl)
		}
	}
}

// spell is the one way a place may say a list: its value kind, the element kind it
// promises and the JSON it writes for those elements.
func spell(t *testing.T, kind, elem view.ValueKind, value any) string {
	t.Helper()
	return string(kind) + "/" + string(elem) + " " + string(bytes(t, value))
}

func wantCell(t *testing.T, cells []view.Cell, name string) view.Cell {
	t.Helper()
	for _, c := range cells {
		if c.Field == name {
			return c
		}
	}
	t.Fatalf("the list shows no %q cell, so this case would inspect nothing", name)
	return view.Cell{}
}

func wantControl(t *testing.T, controls []view.Control, name string) view.Control {
	t.Helper()
	for _, c := range controls {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("the form shows no %q control, so this case would inspect nothing", name)
	return view.Control{}
}
