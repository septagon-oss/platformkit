package components

// breadcrumb_break_test.go pins what the trail does with a name nobody can
// hyphenate. The frame breaks every word it shows (clShellMain sets
// break-anywhere), which is right for prose and wrong for a breadcrumb: at phone
// width a record's one-token name split into "Dashboa / rd", and the page
// scrolled sideways. The table refuses that rule with BreakNormal and the design
// tool accepts it; the trail now refuses it the same way, item by item, and the
// links behind the current entry are what give way.

import (
	"strings"
	"testing"
)

// listItems returns the opening tag of every <li> in the markup, in order.
func listItems(t *testing.T, markup string) []string {
	t.Helper()
	var out []string
	rest := markup
	for {
		at := strings.Index(rest, "<li")
		if at < 0 {
			return out
		}
		rest = rest[at:]
		end := strings.Index(rest, ">")
		if end < 0 {
			t.Fatalf("unterminated <li>:\n%s", markup)
		}
		out = append(out, rest[:end])
		rest = rest[end:]
	}
}

func TestEveryCrumbBreaksByWordsAndOnlyTheLinksTruncate(t *testing.T) {
	trail := draw(t, Breadcrumb(BreadcrumbProps{Items: []BreadcrumbItem{
		{Label: "Workspace", Href: "/app"},
		{Label: "Notes", Href: "/app/task/notes"},
		{Label: "A-name-nobody-can-hyphenate"},
	}}))
	items := listItems(t, trail)
	if len(items) != 5 {
		t.Fatalf("a three-item trail draws %d <li>, want three items and two separators:\n%s", len(items), trail)
	}
	for _, at := range []int{0, 2} { // the two links
		for _, want := range []string{"break-normal", "truncate", "min-w-0"} {
			if !strings.Contains(items[at], want) {
				t.Errorf("the link crumb %q lacks %q: a crumb that inherits break-anywhere splits a name mid-word", items[at], want)
			}
		}
	}
	for _, at := range []int{1, 3} { // the separators
		if !strings.Contains(items[at], "flex-shrink-0") {
			t.Errorf("the separator %q may be squeezed out of the trail", items[at])
		}
	}
	current := items[4]
	if !strings.Contains(current, `aria-current="page"`) {
		t.Fatalf("the last crumb is not the current one: %s", current)
	}
	for _, want := range []string{"break-normal", "min-w-0"} {
		if !strings.Contains(current, want) {
			t.Errorf("the current crumb %q lacks %q", current, want)
		}
	}
	// The name the person came to read is the one thing on the page that is never clipped:
	// truncate is white-space: nowrap, which would stop the last item wrapping by words.
	if strings.Contains(current, "truncate") {
		t.Errorf("the current crumb %q truncates, so a long record name loses the end of its own name", current)
	}
	// min-w-0 is not decoration: a flex item's min-width is its content's own minimum, which
	// is how one long token pushed the page sideways.
	for _, item := range items {
		if strings.Contains(item, "break-anywhere") {
			t.Errorf("a crumb asks for the frame's mid-word break after all: %s", item)
		}
	}
}
