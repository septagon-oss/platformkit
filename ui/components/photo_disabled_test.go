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

func TestDisabledPhotoCollectionsDoNotOfferImageNavigation(t *testing.T) {
	// Reuse the gallery's valid, localized media instead of maintaining a second fixture.
	var props c.PhotoGalleryProps
	for _, example := range examples.Gallery() {
		if example.ID == "pk-ui.component.photo-gallery/default-en" {
			description, err := example.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(description.Props, &props); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	props.ID = "photos-disabled-navigation"
	if len(props.Items) == 0 {
		t.Fatal("photo gallery fixture missing")
	}
	check := func(t *testing.T, node g.Node, disabled bool) {
		t.Helper()
		var out strings.Builder
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
		root, err := nethtml.Parse(strings.NewReader(out.String()))
		if err != nil {
			t.Fatal(err)
		}
		links := 0
		var walk func(*nethtml.Node)
		walk = func(n *nethtml.Node) {
			for _, attr := range n.Attr {
				if attr.Key == "inert" {
					return // An inert subtree offers no native navigation.
				}
			}
			if n.Type == nethtml.ElementNode && n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						for _, photo := range props.Items {
							if attr.Val == photo.Href {
								links++
							}
						}
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(root)
		if !disabled && links != len(props.Items) {
			t.Fatalf("enabled collection has %d image links; want %d", links, len(props.Items))
		}
		if disabled && links != 0 {
			t.Errorf("disabled collection still offers %d native image links", links)
		}
	}
	for _, disabled := range []bool{false, true} {
		props.Disabled = disabled
		t.Run("gallery/"+map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) { check(t, c.PhotoGallery(props), disabled) })
		t.Run("masonry/"+map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			check(t, c.Masonry(c.MasonryProps{ComponentProps: props.ComponentProps, Label: props.Label, Items: props.Items}), disabled)
		})
	}
}
