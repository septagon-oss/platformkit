package components

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

func TestSidebarWrappersDeclareTheirOwnForeground(t *testing.T) {
	for _, flavor := range []string{"admin", "content"} {
		for _, custom := range []bool{false, true} {
			p := SidebarProps{Flavor: flavor, BrandLabel: "Acme", Current: "/active", Sections: []SidebarSection{{Items: []SidebarItem{
				{Label: "Active", Href: "/active", Children: []SidebarItem{{Label: "Child", Href: "/child"}}},
				{Label: "Idle", Href: "/idle"},
			}}}}
			slots := SidebarSlots{}
			if custom {
				slots.Brand = []g.Node{g.Text("A custom brand")}
			}
			doc, err := html.Parse(strings.NewReader(draw(t, SidebarWithSlots(p, slots))))
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			for n := range doc.Descendants() {
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				_, brand := attrs["data-sidebar-brand"]
				_, item := attrs["data-sidebar-item"]
				if !brand && !item {
					continue
				}
				seen++
				want := "text-fg-on-inverse"
				if flavor == "content" {
					want = "text-fg-secondary"
					if brand {
						want = "text-fg-primary"
					} else if attrs["data-active"] == "true" {
						want = "text-fg-brand"
					}
				}
				if !hasClass(attrs["class"], want) || strings.Count(attrs["class"], "text-fg-") != 1 {
					t.Errorf("%s wrapper custom=%v: %v, want exactly %s", flavor, custom, attrs, want)
				}
			}
			if seen != 4 {
				t.Fatalf("measured %d wrappers, want brand and all three nav items", seen)
			}
		}
	}
}

// TestTheSidebarColumnNamesTheInkItsDescendantsInherit checks the colour the
// navigation inherits rather than the ones it is given. The column is the element
// that paints the ground behind the nav, and every descendant that names no colour
// of its own — the <li> around a nav item, a plain <span>, a caller's slot — reads
// through it: on the admin flavour the page's foreground token on that inverse
// ground measures 1.02:1, so the column names the ink that reads on it. The content
// column paints SurfacePrimary, where the inherited ink already reads, and naming a
// colour there would follow a caller's slot onto a ground nobody chose.
func TestTheSidebarColumnNamesTheInkItsDescendantsInherit(t *testing.T) {
	for _, tc := range []struct {
		flavor, surface, ink string
	}{
		{"admin", "bg-surface-inverse", "text-fg-on-inverse"},
		{"content", "bg-surface-primary", ""},
	} {
		p := SidebarProps{Flavor: tc.flavor, BrandLabel: "Acme", Current: "/active", Sections: []SidebarSection{{Items: []SidebarItem{
			{Label: "Active", Href: "/active"}, {Label: "Idle", Href: "/idle"},
		}}}}
		doc, err := html.Parse(strings.NewReader(draw(t, SidebarWithSlots(p, SidebarSlots{}))))
		if err != nil {
			t.Fatal(err)
		}
		painted := ""
		columns := 0
		for n := range doc.Descendants() {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if n.Data != "div" || !hasClass(attrs["class"], tc.surface) {
				continue
			}
			columns++
			painted = attrs["class"]
		}
		if columns != 1 {
			t.Fatalf("%s flavour paints %d divs with %s, want the navigation's one column", tc.flavor, columns, tc.surface)
		}
		var foregrounds []string
		for _, class := range strings.Fields(painted) {
			if strings.HasPrefix(class, "text-fg-") {
				foregrounds = append(foregrounds, class)
			}
		}
		want := []string{tc.ink}
		if tc.ink == "" {
			want = nil
		}
		if !slices.Equal(foregrounds, want) {
			t.Errorf("%s column paints %q: want the foreground %v and no other", tc.flavor, painted, want)
		}
	}
}
