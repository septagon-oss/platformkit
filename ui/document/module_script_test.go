package document_test

import (
	"strings"
	"testing"

	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/document"
)

// TestAPageLoadsOnlyTheModulesItNames pins the per-page half of the editor's
// page weight claim: a view that names a module gets exactly one module script
// tag for it under the shell's asset prefix, and a view that names none renders
// no module tag at all, whatever controllers the chrome loads on every page.
func TestAPageLoadsOnlyTheModulesItNames(t *testing.T) {
	t.Parallel()
	c := chrome()
	r := document.Request{Path: "/app/content/contents/new"}

	with := render(t, document.Document(c, r, document.View{Title: "New", Modules: []string{"js/richtext.js"}}, h.Main()))
	if n := strings.Count(with, `<script src="/admin/assets/js/richtext.js" type="module">`); n != 1 {
		t.Fatalf("a view naming one module renders %d module tags for it:\n%s", n, with)
	}

	without := render(t, document.Document(c, r, document.View{Title: "List"}, h.Main()))
	if strings.Contains(without, `type="module"`) || strings.Contains(without, "richtext.js") {
		t.Fatalf("a view naming no module renders a module tag:\n%s", without)
	}
	for _, controller := range c.Scripts {
		if !strings.Contains(without, "/admin/assets/js/"+controller) {
			t.Fatalf("the chrome's controller %s is missing from the page without modules", controller)
		}
	}
}
