package view_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAPageHoldsItsOwnCopyOfTheValuesItWasGiven is the promise a Page makes to both of
// the Go values it sits between: the map and the list a row carried reach the page as
// copies, so a shell that holds a page never watches the caller's next write change what
// it shows, and a caller never watches a painter change the row it read. The copies are
// what make one page mean one thing to every shell that reads it at the same time; a
// shared map would let the same page bytes say two things, and no shell could tell which
// it was handed.
func TestAPageHoldsItsOwnCopyOfTheValuesItWasGiven(t *testing.T) {
	t.Parallel()
	meta := map[string]any{"origin": "import", "stamp": "05:33"}
	labels := []any{"urgent", "night"}
	row := map[string]any{"id": "t1", "title": "Write", "status": "open",
		"meta": meta, "labels": labels, "revision": json.Number("3")}

	in := input(view.KindRecord, []map[string]any{row}, nil)
	page := build(t, in)
	first := string(bytes(t, page))
	if !strings.Contains(first, `"origin"`) {
		t.Fatal("the record page carries no map value, so nothing here would notice a shared one")
	}

	// The caller writes the row it handed over, after the build returned.
	meta["origin"] = "written"
	meta["added"] = "after the build"
	delete(meta, "stamp")
	labels[0] = "written"
	if got := string(bytes(t, page)); got != first {
		t.Errorf("the page shows what the caller wrote after Build returned:\n%s\n%s", first, got)
	}

	// A painter writes the page it was handed: the same page value, reached through its
	// own members, must not reach back into the input Build was given.
	before := string(bytes(t, in))
	for _, section := range page.Sections {
		for _, field := range section.Fields {
			if shown, ok := field.Value.(map[string]any); ok {
				shown["origin"] = "painted"
			}
			if shown, ok := field.Value.([]any); ok && len(shown) > 0 {
				shown[0] = "painted"
			}
		}
	}
	if after := string(bytes(t, in)); after != before {
		t.Errorf("writing the page wrote the caller's input:\n%s\n%s", before, after)
	}
}
