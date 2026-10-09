package components_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	nethtml "golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

// A disabled card renders its destination as a labelled non-link, the bar
// shared-web-components.md sets for every disabled navigation choice: no anchor
// inside it carries the card's href.
func TestDisabledCardsOfferNoDestination(t *testing.T) {
	props := func(t *testing.T, id string, into any) {
		t.Helper()
		for _, example := range examples.Gallery() {
			if example.ID != id {
				continue
			}
			description, err := example.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(description.Props, into); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("gallery example %s missing", id)
	}
	links := func(t *testing.T, node g.Node, href string) int {
		t.Helper()
		var out strings.Builder
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
		root, err := nethtml.Parse(strings.NewReader(out.String()))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		var walk func(*nethtml.Node)
		walk = func(n *nethtml.Node) {
			for _, attr := range n.Attr {
				if attr.Key == "inert" {
					return
				}
			}
			if n.Type == nethtml.ElementNode && n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && attr.Val == href {
						count++
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(root)
		return count
	}
	check := func(t *testing.T, render func(disabled bool) g.Node, href string) {
		t.Helper()
		if n := links(t, render(false), href); n == 0 {
			t.Fatalf("enabled card offers no link to %s", href)
		}
		if n := links(t, render(true), href); n != 0 {
			t.Errorf("disabled card still offers %d links to %s", n, href)
		}
	}
	t.Run("product-card", func(t *testing.T) {
		var p c.ProductCardProps
		props(t, "pk-ui.component.product-card/default-en", &p)
		check(t, func(disabled bool) g.Node { p.Disabled = disabled; return c.ProductCard(p) }, p.Href)
	})
	t.Run("stat-tile", func(t *testing.T) {
		var p c.StatTileProps
		props(t, "pk-ui.component.stat-tile/default-en", &p)
		p.Href = "/metrics/sample"
		check(t, func(disabled bool) g.Node { p.Disabled = disabled; return c.StatTile(p) }, p.Href)
	})
}
