package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// The generated list renders through DataList; the door it draws in the toolbar
// is still decided by the operations the resource mounted, not by the slot it
// now sits in.
func TestGeneratedDataListDrawsCreateOnlyWhereTheResourceOffersIt(t *testing.T) {
	rows := []map[string]any{{"id": "n-1", "title": "First"}}

	all := note()
	out := render(t, resource.List(all, opts, rows, 1, 1, "title", true).Body)
	if !strings.Contains(out, `data-component="data-list"`) {
		t.Fatalf("the generated list is not a DataList: %s", out)
	}
	if !strings.Contains(out, `href="/app/note/notes/new"`) {
		t.Fatalf("a writable resource offering every operation lost its create door: %s", out)
	}

	readOnly := note()
	readOnly.Operations = []string{"list", "read"}
	out = render(t, resource.List(readOnly, opts, rows, 1, 1, "title", true).Body)
	if !strings.Contains(out, `data-component="data-list"`) || !strings.Contains(out, `href="/app/note/notes/n-1"`) {
		t.Fatalf("the list-and-read resource did not render its rows: %s", out)
	}
	if strings.Contains(out, `/app/note/notes/new`) {
		t.Fatalf("a resource that mounted no create route drew a create door: %s", out)
	}
	if strings.Contains(out, `type="checkbox"`) || strings.Contains(out, `<form`) {
		t.Fatalf("a resource with no write operation and no command drew a write control: %s", out)
	}
}
