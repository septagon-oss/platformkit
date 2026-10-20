package components

// breadcrumb_break_test.go pins what the trail does with a name nobody can
// hyphenate. The frame breaks every word it shows (clShellMain sets
// break-anywhere, and the property inherits), and at phone width a record's
// one-token name split into "Dashboa / rd" while a longer one pushed the page
// sideways. What did that was never the break rule but the row: a flex row that
// never wraps squeezes every crumb below the width of its own name, and a box
// narrower than its longest word is a box that breaks words in half. So the trail
// wraps. No crumb carries a break rule of its own — that is the frame's, on the
// region (see TestTheFrameBreaksATokenNothingCanHyphenate, which refuses a break
// rule on any component inside the region because it is what the design tool reads),
// and the links behind the current entry are still what give way.

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

func TestTheTrailWrapsAndOnlyTheLinksTruncate(t *testing.T) {
	trail := draw(t, Breadcrumb(BreadcrumbProps{Items: []BreadcrumbItem{
		{Label: "Workspace", Href: "/app"},
		{Label: "Notes", Href: "/app/task/notes"},
		{Label: "A-name-nobody-can-hyphenate"},
	}}))
	items := listItems(t, trail)
	if len(items) != 5 {
		t.Fatalf("a three-item trail draws %d <li>, want three items and two separators:\n%s", len(items), trail)
	}
	// The cure, in one rule: a row that wraps gives a crumb a line of its own instead of a share
	// of one, which is the difference between a name wrapped by words and a name split mid-word.
	ol := classOf(t, trail, "<ol", "ol")
	if !strings.Contains(ol, "flex-wrap") {
		t.Errorf("the trail %q does not wrap, so its crumbs are squeezed and break mid-word", ol)
	}
	for _, at := range []int{0, 2} { // the two links
		for _, want := range []string{"truncate", "min-w-0"} {
			if !strings.Contains(items[at], want) {
				t.Errorf("the link crumb %q lacks %q: a link gives way with an ellipsis, not by splitting a name", items[at], want)
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
	if !strings.Contains(current, "min-w-0") {
		t.Errorf("the current crumb %q lacks min-w-0: a flex item's min-width is its content's own minimum, which is how one long token pushed the page sideways", current)
	}
	// The name the person came to read is the one thing on the page that is never clipped:
	// truncate is white-space: nowrap, which would stop the last item wrapping by words.
	if strings.Contains(current, "truncate") {
		t.Errorf("the current crumb %q truncates, so a long record name loses the end of its own name", current)
	}
	// And no crumb takes the break rule into its own hands, either direction: the region owns it,
	// and a component that declares one is text the design tool will not project for a client.
	for _, item := range append(items, ol) {
		for _, rule := range []string{"break-anywhere", "break-normal"} {
			if strings.Contains(item, rule) {
				t.Errorf("a crumb declares %q while the frame's content region already does: %s", rule, item)
			}
		}
	}
}
