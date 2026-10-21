package components

// sidebar_inner_test.go pins the blank band between the sidebar and the content.
// The <aside> is the element that carries a width — 16rem expanded, 4rem collapsed,
// from the large breakpoint up — and it paints nothing itself: the inverse column a
// person sees is two levels inside it. That column's wrapper is a flex item in a
// flex row, so with no width of its own it sized to its content and the leftover of
// the aside showed the page's own background. One rule closes it, and it has to hold
// in both flavours and both widths, because a fix that works in one of four states
// is the same bug with a smaller screenshot.

import (
	"strings"
	"testing"
)

func TestTheSidebarColumnFillsTheAsideThatCarriesItsWidth(t *testing.T) {
	cases := []struct {
		flavour   string
		collapsed bool
		aside     string // the width the <aside> itself carries
	}{
		{"admin", false, "lg:w-64"},
		{"admin", true, "lg:w-16"},
		// The content flavour is a column, so its child stretched anyway and it names no
		// breakpoint width at all — which is why only the admin frame ever showed the band.
		{"content", false, ""},
		{"content", true, ""},
	}
	for _, tc := range cases {
		markup := draw(t, Sidebar(SidebarProps{
			Flavor: tc.flavour, Collapsed: tc.collapsed, Collapsible: true,
			BrandLabel: "End to end", Items: []SidebarItem{{Label: "Dashboard", Href: "/app"}},
		}))
		if aside := classOf(t, markup, "<aside", "aside"); tc.aside != "" && !strings.Contains(aside, tc.aside) {
			t.Errorf("the %s sidebar collapsed=%v carries %q, want it to hold %q", tc.flavour, tc.collapsed, aside, tc.aside)
		}
		open := classOf(t, markup, `data-component="sidebar"`, "div")
		if !strings.Contains(open, "w-full") {
			t.Errorf("the %s sidebar collapsed=%v opens a column classed %q: with no width it sizes to its content and the aside's leftover shows the page behind it",
				tc.flavour, tc.collapsed, open)
		}
	}
}
