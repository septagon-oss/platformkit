package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnEnumValueOutsideTheDeclaredSetWearsNeutralInItsFieldAndItsCell holds that the
// two readings a declared field can give a person stay visibly different in both places
// a Page shows a field. A value the author named wears the colour the author gave it; a
// value nobody named wears "neutral", the colour that says "outside the set", and never
// the silence of a field that declares no values at all. These rows show one status with
// a colour of its own and one outside the declared set, in the record's field and in the
// list's cell: the two may not meet, because a page on which every status reads the same
// tells a person nothing about which task is finished.
func TestAnEnumValueOutsideTheDeclaredSetWearsNeutralInItsFieldAndItsCell(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": "t1", "title": "Ship", "status": "done"},
		{"id": "t2", "title": "Write", "status": "in_progress"},
	}
	// The colour each reading gives it: the fixture declares one for "done" alone.
	wanted := map[string]string{"done": "success", "in_progress": "neutral"}

	for _, at := range []struct {
		surface string
		kind    view.Kind
		shown   func(view.Input) []view.Field
	}{
		{"the record", view.KindRecord, func(in view.Input) []view.Field {
			var out []view.Field
			for _, s := range build(t, in).Sections {
				out = append(out, s.Fields...)
			}
			return out
		}},
		// A list shows the same field as a cell, whose reading members mean what
		// Field's mean, so the rule a person reads on the list is the same rule.
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
		shown := map[string]string{}
		for _, row := range rows {
			value := row["status"].(string)
			for _, f := range at.shown(input(at.kind, []map[string]any{row}, nil)) {
				if f.Name != "status" {
					continue
				}
				shown[value] = f.Tone
				if f.Tone != wanted[value] {
					t.Errorf("%s shows status %q with tone %q, want the %q its reading gives it",
						at.surface, value, f.Tone, wanted[value])
				}
			}
		}
		if len(shown) != len(rows) {
			t.Fatalf("%s showed %d of the %d statuses, so this case inspected too little",
				at.surface, len(shown), len(rows))
		}
		if shown["done"] == shown["in_progress"] {
			t.Errorf("%s colours the declared value and the value outside its set alike as %q",
				at.surface, shown["done"])
		}
	}
}
