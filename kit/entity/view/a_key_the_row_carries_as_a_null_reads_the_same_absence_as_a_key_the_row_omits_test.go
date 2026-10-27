package view_test

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAKeyTheRowCarriesAsANullReadsTheSameAbsenceAsAKeyTheRowOmits holds absent and
// empty apart where the row, not the declaration, decides: a key the read carried as
// JSON null and a key it never carried are one fact — nothing was stored — so both
// read absent, with a null value and one text, in a record's field, a table's cell and
// a form's control. A field a command cleared arrives as the first and a row written
// before the field existed as the second; a page that read the first as a value would
// show "", 0 or [] beside a field the tenant emptied, which is the one mistake a shell
// cannot undo afterwards.
func TestAKeyTheRowCarriesAsANullReadsTheSameAbsenceAsAKeyTheRowOmits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		declared string
		field    entity.Field
	}{
		{"a string", entity.Field{Name: "note", Type: entity.TypeString}},
		{"an integer", entity.Field{Name: "count", Type: entity.TypeInt}},
		{"a list", entity.Field{Name: "tags", Type: entity.TypeList, Elem: entity.TypeString}},
	} {
		s := taskSchema()
		s.Fields = append(s.Fields, tc.field)
		carried := spellingsAtEveryKind(t, s, tc.field.Name, map[string]any{"id": "t1", "title": "n", tc.field.Name: nil})
		omitted := spellingsAtEveryKind(t, s, tc.field.Name, map[string]any{"id": "t1", "title": "n"})
		for _, place := range []string{"field", "cell", "control"} {
			if carried[place] != omitted[place] {
				t.Errorf("a %s the row carries as null reads %s and the same field the row omits reads %s in its %s: each stored nothing, and neither may read a value",
					tc.declared, carried[place], omitted[place], place)
			}
			if want := "absent|null|" + omitted["missing"]; place != "control" && omitted[place] != want {
				t.Errorf("a %s the row omits reads %s in its %s, want %s", tc.declared, omitted[place], place, want)
			}
		}
		if got := omitted["control"]; got != "absent|null|" {
			t.Errorf("a %s the row omits reads %s in its control, want absent with a null value", tc.declared, got)
		}
	}
}

// spellingsAtEveryKind is how one field reads in the three places a Page shows it, one
// line each: its value kind, the JSON of its value and its text, beside the resolved
// missing word so a case can tell a shared "" from a shared dash. A control shows no
// text, so its line ends after the value.
func spellingsAtEveryKind(t *testing.T, s entity.Schema, name string, row map[string]any) map[string]string {
	t.Helper()
	in := input(view.KindRecord, []map[string]any{row}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	in.State.Zone = time.UTC

	out := map[string]string{}
	field, ok := recordCells(t, in)[name]
	if !ok {
		t.Fatalf("the record shows no %q, so this case would inspect nothing", name)
	}
	out["missing"] = field.Text
	out["field"] = line(t, field.ValueKind, field.Value, field.Text)

	in.State.Kind = view.KindList
	page := build(t, in)
	for _, c := range page.Table.Rows[0].Cells {
		if c.Field == name {
			out["cell"] = line(t, c.ValueKind, c.Value, c.Text)
		}
	}

	in.State.Kind = view.KindForm
	in.State.Form = view.FormState{Title: "Edit " + name}
	for _, c := range build(t, in).Form.Controls {
		if c.Name == name {
			out["control"] = line(t, c.ValueKind, c.Value, "")
		}
	}
	for _, place := range []string{"cell", "control"} {
		if _, shown := out[place]; !shown {
			t.Fatalf("the %s page shows no %q, so this case would inspect nothing", place, name)
		}
	}
	return out
}

// line is one place's spelling of one field, on one line.
func line(t *testing.T, kind view.ValueKind, value any, text string) string {
	t.Helper()
	return string(kind) + "|" + string(bytes(t, value)) + "|" + text
}
