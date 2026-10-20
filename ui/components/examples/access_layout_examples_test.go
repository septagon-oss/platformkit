package examples_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

// The native coverage inventory names these two examples' refusals by id, so
// each must stay in the gallery once, rendering the component it is named for.
func TestTheGalleryShowsTheAccessPageAndTheDisclosureOnceEach(t *testing.T) {
	want := map[string][]string{
		"pk-ui.component.standalone/default": {"<main", "Sign in"},
		"pk-ui.component.disclosure/default": {"<details", `data-component="disclosure"`, "<summary", "Coordinator", "Review permissions"},
	}
	seen := map[string]int{}
	for _, example := range examples.Gallery() {
		marks, ok := want[example.ID]
		if !ok {
			continue
		}
		seen[example.ID]++
		var html strings.Builder
		if err := example.Node.Render(&html); err != nil {
			t.Fatal(err)
		}
		for _, mark := range marks {
			if !strings.Contains(html.String(), mark) {
				t.Errorf("%s renders without %q: %s", example.ID, mark, html.String())
			}
		}
	}
	for id := range want {
		if seen[id] != 1 {
			t.Errorf("gallery holds %s %d times, want once", id, seen[id])
		}
	}
}

// A disclosure is closed unless its caller opens it, so a permission form it
// holds is never shown to a person who did not ask for it.
func TestADisclosureOpensOnlyWhenAsked(t *testing.T) {
	render := func(open bool) string {
		var html strings.Builder
		if err := c.Disclosure(c.DisclosureProps{Label: "Coordinator", Open: open}).Render(&html); err != nil {
			t.Fatal(err)
		}
		return html.String()
	}
	if closed := render(false); strings.Contains(closed, " open") {
		t.Fatalf("closed disclosure renders open: %s", closed)
	}
	if opened := render(true); !strings.Contains(opened, " open") {
		t.Fatalf("opened disclosure renders closed: %s", opened)
	}
}
