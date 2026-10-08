package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestGeneratedListRendersAnEmptyPageAfterTheLastPageDisappears(t *testing.T) {
	// A person follows a bookmarked third page after another actor deletes its
	// last row. The read legitimately returns no rows and a two-page total.
	view := resource.List(note(), opts, nil, int64(resource.PerPage+1), 3, "title", false)
	out, err := document.Render(g.Group(view.Body))
	if err != nil {
		t.Fatalf("an empty successful page must retain navigation, not fail rendering: %v", err)
	}
	for _, want := range []string{"No notes yet.", `href="/app/note/notes?page=1&amp;sort=title"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("empty page lost %q: %s", want, out)
		}
	}
}
