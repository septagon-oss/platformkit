package resource_test

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/ui/document"
)

// A control's description is the element its own ID names: the aria-describedby
// of a command argument starts with that control's ID, so two commands sharing an
// argument name never describe each other's box.
func TestCommandControlDescriptionsFollowTheirOwnControlID(t *testing.T) {
	view := commandDetail(sharedArgumentCommands(), "one", commandLocales())
	out, err := document.Render(document.Document(document.Chrome{}, document.Request{}, view, document.Bare(view.Body)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for n := range doc.Descendants() {
		if id := commandAttribute(n, "id"); id != "" {
			ids[id] = true
		}
	}
	described := 0
	for n := range doc.Descendants() {
		ref := commandAttribute(n, "aria-describedby")
		if ref == "" || !strings.HasPrefix(commandAttribute(n, "id"), "pk-command-") {
			continue
		}
		described++
		for _, id := range strings.Fields(ref) {
			if !ids[id] || !strings.HasPrefix(id, commandAttribute(n, "id")) {
				t.Errorf("control %q describes itself with %q", commandAttribute(n, "id"), id)
			}
		}
	}
	if described == 0 {
		t.Fatal("no scoped command control carried a description")
	}
}
