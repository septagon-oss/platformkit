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
//
// Naming the ink is half the guarantee: it reaches a region only from inside the element
// that paints the ground, so the brand, the navigation and a caller's footer have to sit
// within that element rather than merely carry the classes that look right beside it. A
// region moved out of the column keeps its own colour and reads on whatever ground it lands
// on — which is why a footer naming its own ink would hide the move from a case that only
// reads the column's classes.
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
		// The regions are composed as a caller composes them, because a caller's slot is the
		// content that arrives with no colour of its own and reads through the column's.
		slots := SidebarSlots{
			Brand:  []g.Node{g.Text("Acme Operations")},
			Footer: []g.Node{g.Text("Signed in as Ada")},
		}
		doc, err := html.Parse(strings.NewReader(draw(t, SidebarWithSlots(p, slots))))
		if err != nil {
			t.Fatal(err)
		}
		painted := ""
		columns := 0
		var column *html.Node
		for n := range doc.Descendants() {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if n.Data != "div" || !hasClass(attrs["class"], tc.surface) {
				continue
			}
			columns++
			column = n
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
		drawn := map[string]int{}
		inherited := map[string]int{}
		for n := range doc.Descendants() {
			region := sidebarInkedRegion(n)
			if region == "" {
				continue
			}
			drawn[region]++
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				if parent == column {
					inherited[region]++
					break
				}
			}
		}
		for _, region := range []string{"brand", "navigation", "footer"} {
			if drawn[region] != 1 || inherited[region] != 1 {
				t.Errorf("%s flavour draws %d %s region(s), %d of them inside the column that paints the ground: want one that inherits its ink", tc.flavor, drawn[region], region, inherited[region])
			}
		}
	}
}

// sidebarInkedRegion names the part of the sidebar a caller composes without a colour of
// its own — the brand, the navigation, the footer — or "" for any other node.
func sidebarInkedRegion(n *html.Node) string {
	for _, a := range n.Attr {
		switch a.Key {
		case "data-sidebar-brand":
			return "brand"
		case "data-sidebar-footer":
			return "footer"
		}
	}
	if n.Data == "nav" {
		return "navigation"
	}
	return ""
}
