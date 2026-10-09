package examples_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/ui/components/examples"
)

func TestTheSharedWebBriefHasAnExecutableExampleForEveryFamily(t *testing.T) {
	gallery := examples.Gallery()
	// These are the public component identities specified by this delivery,
	// not a total pinned against unrelated additions to the repository.
	for _, name := range []string{
		"empty-state", "notice", "data-list", "detail-sheet", "side-panel", "timeline",
		"stepper", "date-strip", "slot-picker", "calendar", "product-card", "option-chips",
		"quantity-input", "buy-bar", "cart", "order-summary", "pricing-tiers", "plan-comparison",
		"map-view", "photo-gallery", "masonry", "sparkline", "area-chart", "bar-chart", "stat-tile",
	} {
		t.Run(name, func(t *testing.T) {
			for _, example := range gallery {
				if example.ComponentID != "pk-ui.component."+name {
					continue
				}
				description, err := example.Describe()
				if err != nil {
					t.Fatal(err)
				}
				if description.HTML == "" || !description.PropsEditable {
					t.Fatal("the documented component needs a rendered typed constructor example")
				}
				return
			}
			t.Fatalf("the brief's %s family has no executable Gallery example", name)
		})
	}
}
