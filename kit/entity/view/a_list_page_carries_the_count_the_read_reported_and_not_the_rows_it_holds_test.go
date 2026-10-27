package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAListPageCarriesTheCountTheReadReportedNotTheRowsItHolds keeps a list's two
// numbers apart: the rows the page holds, and the count the read reported for the whole
// result. A window asks for a slice of a larger answer, so the two differ whenever a
// person is on any page but the last one, and a page that answers with the rows in hand
// tells them they have seen everything while the next page sits unread, and gives a
// shell nothing to build a pager from. A read that took no count says so, and the page
// carries that answer rather than inventing one.
func TestAListPageCarriesTheCountTheReadReportedNotTheRowsItHolds(t *testing.T) {
	t.Parallel()

	// The rows the package's own list fixture carries, read as a window into a larger
	// result: two rows in hand out of forty-one the read counted.
	truncated := listInput()
	truncated.Data.Window = view.Window{Offset: 2, Limit: 2}
	truncated.Data.Total = 41
	truncated.Data.Counted = true
	page := build(t, truncated)

	if shown := len(page.Table.Rows); shown != 2 {
		t.Fatalf("the window showed %d rows of the 2 it asked for out of the 41 the read counted, so this case compares nothing",
			shown)
	}
	if page.Table.Total != 41 || !page.Table.Counted {
		t.Errorf("the page answers %d rows counted=%v where the read reported 41 of a counted result: a list carries the count the read gave, not the rows it holds",
			page.Table.Total, page.Table.Counted)
	}
	if page.Table.Window != (view.Window{Offset: 2, Limit: 2}) {
		t.Errorf("the page names window %+v, want the window the read asked for", page.Table.Window)
	}

	// The same rows, read without a count: the page says none, rather than answering
	// with what it happens to hold.
	uncounted := listInput()
	uncounted.Data.Total = 0
	uncounted.Data.Counted = false
	quiet := build(t, uncounted)
	if quiet.Table.Counted || quiet.Table.Total != 0 {
		t.Errorf("the page answers %d rows counted=%v where the read gave no count: a page may not invent one",
			quiet.Table.Total, quiet.Table.Counted)
	}
	if len(quiet.Table.Rows) != len(page.Table.Rows) {
		t.Fatalf("the two reads were not the same rows: %d then %d, so this case compared different lists",
			len(page.Table.Rows), len(quiet.Table.Rows))
	}
}
