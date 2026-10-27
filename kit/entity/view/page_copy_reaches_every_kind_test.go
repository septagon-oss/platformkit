package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestEveryPageKindHoldsItsValuesByCopy is the copy promise outside the record page.
// A list page carries the caller's own map and list in its table cells and a form page
// in its control defaults, and each must hold them by copy for the same reason the
// record's fields do: a shell that holds a page never watches the row it read change
// what the page shows, and never watches a painting change that row. A page spells its
// prose from the value, so a kind that reached a shell with prose alone would pass for
// the wrong reason — each case therefore refuses first that the values it is about to
// write through really arrived in the page.
func TestEveryPageKindHoldsItsValuesByCopy(t *testing.T) {
	t.Parallel()
	for _, kind := range []view.Kind{view.KindList, view.KindForm} {
		t.Run(string(kind), func(t *testing.T) {
			meta := map[string]any{"origin": "import", "stamp": "05:33"}
			labels := []any{"urgent", "night"}
			row := map[string]any{"id": "t1", "title": "Write", "status": "open",
				"meta": meta, "labels": labels, "revision": json.Number("3")}
			in := input(kind, []map[string]any{row}, nil)
			if kind == view.KindForm {
				in.State.Form = view.FormState{Create: true, Title: "New task"}
			}

			page := build(t, in)
			first := string(bytes(t, page))

			// The caller writes the map and the list it handed over.
			meta["origin"] = "written"
			delete(meta, "stamp")
			labels[0] = "written"
			if got := string(bytes(t, page)); got != first {
				t.Errorf("the %s page shows what the caller wrote after Build returned:\n%s\n%s", kind, first, got)
			}

			// A painter writes the page it was handed.
			before := string(bytes(t, in))
			painted := paint(t, page, kind)
			if painted < 2 {
				t.Fatalf("the %s page reached this case with %d values a painter could write, so a shared one would go unnoticed", kind, painted)
			}
			if after := string(bytes(t, in)); after != before {
				t.Errorf("writing the %s page wrote the caller's input:\n%s\n%s", kind, before, after)
			}
		})
	}
}

// paint writes every map and list the page shows at its kind's own place — a list
// page's cells, a form page's control values — and reports how many it reached.
func paint(t *testing.T, page view.Page, kind view.Kind) int {
	t.Helper()
	reached := 0
	write := func(v any) {
		switch shown := v.(type) {
		case map[string]any:
			shown["origin"] = "painted"
			reached++
		case []any:
			if len(shown) > 0 {
				shown[0] = "painted"
				reached++
			}
		}
	}
	switch kind {
	case view.KindList:
		for _, row := range page.Table.Rows {
			for _, cell := range row.Cells {
				write(cell.Value)
			}
		}
	case view.KindForm:
		for _, control := range page.Form.Controls {
			write(control.Value)
		}
	}
	return reached
}
