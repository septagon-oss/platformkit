package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnEnumValueOutsideTheDeclaredSetKeepsItsOwnTextInItsFieldAndItsCell holds
// the words of the rule beside its colour: a value the author named wears the
// label they wrote for it, and a value nobody named keeps the text the row
// carries, in the record's field and in the list's cell. The cell is the one
// place a person meets such a value at scale, and a cell that prints the words
// for an absent value sends them looking for a record that holds one. The list's
// text is read against the field's own, so a cell may not answer with anything
// the field did not show for the same row.
func TestAnEnumValueOutsideTheDeclaredSetKeepsItsOwnTextInItsFieldAndItsCell(t *testing.T) {
	t.Parallel()

	const declared, outside = "done", "in_progress"
	rows := []map[string]any{
		{"id": "t1", "title": "Ship", "status": declared},
		{"id": "t2", "title": "Write", "status": outside},
	}
	// The words each value is shown with: the fixture labels "open" alone, so
	// "done" arrives humanised and "in_progress" arrives as the row's own text.
	wanted := map[string]string{declared: "Done", outside: "in_progress"}

	surfaces := map[string]map[string]string{}
	for _, at := range []struct {
		name  string
		kind  view.Kind
		shown func(view.Input) []view.Field
	}{
		{"the record", view.KindRecord, func(in view.Input) []view.Field {
			var out []view.Field
			for _, s := range build(t, in).Sections {
				out = append(out, s.Fields...)
			}
			return out
		}},
		// A list shows the same field as a cell, whose reading members mean
		// what Field's mean, so the words a person reads on the list are the
		// same words.
		{"the list", view.KindList, func(in view.Input) []view.Field {
			var out []view.Field
			for _, r := range build(t, in).Table.Rows {
				for _, c := range r.Cells {
					out = append(out, view.Field{Name: c.Field, Text: c.Text, Tone: c.Tone})
				}
			}
			return out
		}},
	} {
		texts := map[string]string{}
		for _, row := range rows {
			value := row["status"].(string)
			for _, f := range at.shown(input(at.kind, []map[string]any{row}, nil)) {
				if f.Name != "status" {
					continue
				}
				texts[value] = f.Text
				if f.Text != wanted[value] {
					t.Errorf("%s shows status %q as %q, want %q",
						at.name, value, f.Text, wanted[value])
				}
			}
		}
		if len(texts) != len(rows) {
			t.Fatalf("%s showed the words of %d of the %d statuses, so this case inspected too little",
				at.name, len(texts), len(rows))
		}
		surfaces[at.name] = texts
	}
	for value := range wanted {
		if list, field := surfaces["the list"][value], surfaces["the record"][value]; list != field {
			t.Errorf("status %q reads %q on the list and %q in the record: a cell prints what its field prints for the same row",
				value, list, field)
		}
	}
}
