package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACellWearsTheCaptionItsColumnAndItsFieldWear holds one field's caption to the
// three places a Page shows it: the list's column header, the cell that sits in that
// column, and the record's field. A person scanning a list reads a header once and
// the cell below it many times, so the two saying different things — or the cell
// saying nothing at all — makes them stop and work out what the value means. The
// caption comes from the author's field, so a cell that spelled the field key instead
// would show the reader a name a developer chose.
func TestACellWearsTheCaptionItsColumnAndItsFieldWear(t *testing.T) {
	t.Parallel()

	// The captions this fixture's author wrote, spelled as a person reads them
	// rather than as the schema keys them. Both appear as a column, as a cell in
	// that column and as a record field, which is what this case compares.
	wanted := map[string]string{"status": "Status", "dueAt": "Due at"}
	rows := []map[string]any{
		{"id": "t1", "title": "Write", "status": "open", "dueAt": "2026-07-01T14:12:00Z"},
		{"id": "t2", "title": "Ship", "status": "done", "dueAt": "2026-07-02T09:00:00Z"},
	}

	list := build(t, input(view.KindList, rows, nil))
	headings := map[string]string{}
	for _, c := range list.Table.Columns {
		headings[c.Field] = c.Label
		if want, named := wanted[c.Field]; named && c.Label != want {
			t.Errorf("the %s column is headed %q, want %q", c.Field, c.Label, want)
		}
	}
	cells := 0
	for _, r := range list.Table.Rows {
		if len(r.Cells) != len(list.Table.Columns) {
			t.Fatalf("row %q carries %d cells for %d columns: a shell reads a cell by column",
				r.ID, len(r.Cells), len(list.Table.Columns))
		}
		for i, cell := range r.Cells {
			column := list.Table.Columns[i]
			if cell.Field != column.Field {
				t.Fatalf("row %q cell %d reads %q where its column reads %q: cells are ordered as their columns",
					r.ID, i, cell.Field, column.Field)
			}
			cells++
			if _, named := wanted[cell.Field]; !named {
				continue
			}
			if cell.Label != column.Label {
				t.Errorf("%s of %q wears %q while its column wears %q: a cell wears the caption of the column it sits in",
					cell.Field, r.ID, cell.Label, column.Label)
			}
		}
	}
	if cells != len(rows)*len(list.Table.Columns) {
		t.Fatalf("the list showed %d cells of the %d it owes, so this case inspected too little",
			cells, len(rows)*len(list.Table.Columns))
	}

	record := build(t, input(view.KindRecord, rows[:1], nil))
	fields := 0
	for _, s := range record.Sections {
		for _, f := range s.Fields {
			want, named := wanted[f.Name]
			if !named {
				continue
			}
			fields++
			if f.Label != want {
				t.Errorf("the record shows %s under %q, want %q: one caption in every shell",
					f.Name, f.Label, want)
			}
			if headed, onTheList := headings[f.Name]; !onTheList || headed != f.Label {
				t.Errorf("%s wears %q in the record and %q on the list: a field keeps its caption between the record and the column",
					f.Name, f.Label, headed)
			}
		}
	}
	if fields != len(wanted) {
		t.Fatalf("the record showed the captions of %d of the %d fields this case names, so it inspected too little",
			fields, len(wanted))
	}
}
