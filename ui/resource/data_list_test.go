package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestGeneratedDataListKeepsEmptyCollectionCommandsCountsAndPaging(t *testing.T) {
	out := render(t, resource.List(withCommands(sweepCommand), opts, nil, 120, 3, "-title", false).Body)
	for _, want := range []string{`data-component="data-list"`, "120 notes", "No notes yet.", `>Title<`, `aria-sort="descending"`, `href="/app/note/notes?sort=title"`, `action="/app/note/notes/sweep"`, `href="/app/note/notes?page=2&amp;sort=-title"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated DataList dropped %q: %s", want, out)
		}
	}
	if strings.Index(out, `action="/app/note/notes/sweep"`) > strings.Index(out, `data-component="pagination"`) {
		t.Fatal("collection command moved after the pager")
	}
	if strings.Contains(out, `data-pk-select`) || strings.Contains(out, `type="checkbox"`) {
		t.Fatal("adopting DataList must not invent a generic bulk command")
	}
}
