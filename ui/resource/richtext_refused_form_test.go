package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheRefusedFormSaysWhoseWordsItShows pins the two attributes the browser needs
// in order to keep an unsaved draft alive through a write the server refused, and to
// show a picture it has just stored: the address one file's bytes are read back from,
// and the page's own statement of whether its controls hold the record's text or the
// person's refused submission. A draft is cleared against the record and never
// against an echo, so the echo has to name itself — and a form that cannot say which
// of the two it is showing is a form that throws away the only copy of unsaved words.
func TestTheRefusedFormSaysWhoseWordsItShows(t *testing.T) {
	t.Parallel()
	id := "11111111-1111-1111-1111-111111111111"
	row := map[string]any{"id": id, "title": "Hours", "body": "## Hours"}
	address := `data-richtext-image-src="/api/v1/file/files/{file}/content"`

	for name, page := range map[string]string{
		"the create form": render(t, resource.Form(article(), opts, "/app/article/articles/new", "New", nil, nil, "", true).Body),
		"the edit form":   render(t, resource.Form(article(), opts, "/app/article/articles/"+id+"/edit", "Edit", row, nil, "", false).Body),
	} {
		if !strings.Contains(page, address) {
			t.Errorf("%s does not name where a stored picture's bytes are read back from:\n%s", name, page)
		}
		if strings.Contains(page, "data-richtext-submitted") {
			t.Errorf("%s says it shows a refused submission; it shows the record:\n%s", name, page)
		}
	}

	refused := map[string]any{"title": "Hours", "body": "<script>alert(1)</script>"}
	page := render(t, resource.Form(article(), opts, "/app/article/articles/"+id, "Edit", refused,
		map[string]string{"body": "line 1: raw HTML"}, "line 1: raw HTML", false).Body)
	if !strings.Contains(page, "data-richtext-submitted") {
		t.Errorf("the form redrawn over a refused write does not say whose words it shows:\n%s", page)
	}
	if !strings.Contains(page, address) {
		t.Errorf("the form redrawn over a refused write lost the address a picture is read back from:\n%s", page)
	}
}
