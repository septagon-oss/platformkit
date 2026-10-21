package components

import (
	"strings"
	"testing"
)

// A stacked column's phone sort control sits in a header row of its own, never inside
// another column's header, so a lookup of one column header by its label finds one.
func TestStackedSortControlIsNotInsideAnotherColumnHeader(t *testing.T) {
	t.Parallel()

	out := renderNode(t, Table(TableProps{
		Label:    "Tasks",
		Sortable: true,
		Columns: []TableColumn{
			{Key: "title", Label: "Title"},
			{Key: "due", Label: "Due", Sortable: true, Stacked: true},
		},
		Rows: []TableRow{{ID: "t1", Cells: map[string]any{"title": "A", "due": "x"}}},
	}))

	for _, th := range strings.Split(out, "<th ")[1:] {
		cell := th[:strings.Index(th, "</th>")]
		if strings.Contains(cell, ">Title") && strings.Contains(cell, "Due") {
			t.Fatalf("the Title header carries the Due sort control: %s", cell)
		}
	}
}
