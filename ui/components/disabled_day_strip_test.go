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

// A disabled Calendar or SlotPicker offers no day to follow: its strip of days is
// a set of navigation choices, and a disabled navigation choice is a labelled
// non-link.
func TestDisabledDayStripsOfferNoDay(t *testing.T) {
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
	dayLinks := func(t *testing.T, node g.Node, days []c.DateChoice) int {
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
					for _, day := range days {
						if attr.Key == "href" && day.Href != "" && attr.Val == day.Href {
							count++
						}
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
	check := func(t *testing.T, render func(disabled bool) g.Node, days []c.DateChoice) {
		t.Helper()
		if n := dayLinks(t, render(false), days); n == 0 {
			t.Fatal("enabled component offers no day link")
		}
		if n := dayLinks(t, render(true), days); n != 0 {
			t.Errorf("disabled component still offers %d day links", n)
		}
	}
	t.Run("calendar", func(t *testing.T) {
		var p c.CalendarProps
		props(t, "pk-ui.component.calendar/default-en", &p)
		p.ID = "calendar-disabled-days"
		check(t, func(disabled bool) g.Node { p.Disabled = disabled; return c.Calendar(p) }, p.DateStrip.Days)
	})
	t.Run("slot-picker", func(t *testing.T) {
		var p c.SlotPickerProps
		props(t, "pk-ui.component.slot-picker/default-en", &p)
		p.ID = "slot-picker-disabled-days"
		check(t, func(disabled bool) g.Node { p.Disabled = disabled; return c.SlotPicker(p) }, p.DateStrip.Days)
	})
}
