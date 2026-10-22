package components

import (
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
