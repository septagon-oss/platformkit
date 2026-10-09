package components_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components/examples"
)

func TestDisabledEnhancedViewsPreserveDisplayMetadata(t *testing.T) {
	gallery := examples.Gallery()
	for _, family := range []string{"calendar/day", "calendar/week", "map-view/map"} {
		for _, language := range []string{"en", "pt-PT"} {
			id := "pk-ui.component." + family + "-" + language
			t.Run(id, func(t *testing.T) {
				var example examples.Example
				for _, candidate := range gallery {
					if candidate.ID == id {
						example = candidate
						break
					}
				}
				if example.ID == "" {
					t.Fatal("missing declared gallery example")
				}
				var enabled []map[string]any
				for _, patch := range []string{`{"disabled":false}`, `{"disabled":true}`, `{"disabled":false}`} {
					view, err := example.WithProps([]byte(patch))
					if err != nil {
						t.Fatal(err)
					}
					var out strings.Builder
					if err := view.Node.Render(&out); err != nil {
						t.Fatal(err)
					}
					events, points := engineConfig(t, out.String())
					items := append(events, points...)
					if len(items) == 0 {
						t.Fatal("the engine received no display data")
					}
					for _, item := range items {
						// Navigation is checked separately; every display field must survive.
						delete(item, "url")
						delete(item, "href")
					}
					if enabled == nil {
						enabled = items
					} else if !reflect.DeepEqual(items, enabled) {
						t.Fatalf("%s changed display metadata: got %#v, want %#v", patch, items, enabled)
					}
				}
			})
		}
	}
}
