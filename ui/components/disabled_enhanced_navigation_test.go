package components_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components/examples"
	nethtml "golang.org/x/net/html"
)

func TestDisabledEnhancedViewsDoNotSupplyActiveDestinations(t *testing.T) {
	cases := 0
	for _, example := range examples.Gallery() {
		if example.ID != "pk-ui.component.calendar/day-en" && example.ID != "pk-ui.component.calendar/week-pt-PT" &&
			example.ID != "pk-ui.component.map-view/map-en" && example.ID != "pk-ui.component.map-view/map-pt-PT" {
			continue
		}
		cases++
		t.Run(example.ID, func(t *testing.T) {
			for _, disabled := range []bool{false, true} {
				patch, err := json.Marshal(map[string]bool{"disabled": disabled})
				if err != nil {
					t.Fatal(err)
				}
				edited, err := example.WithProps(patch)
				if err != nil {
					t.Fatal(err)
				}
				var out strings.Builder
				if err := edited.Node.Render(&out); err != nil {
					t.Fatal(err)
				}
				root, err := nethtml.Parse(strings.NewReader(out.String()))
				if err != nil {
					t.Fatal(err)
				}
				links := 0
				var walk func(*nethtml.Node)
				walk = func(node *nethtml.Node) {
					if node.Type == nethtml.ElementNode && node.Data == "template" {
						return
					}
					for _, attr := range node.Attr {
						if attr.Key == "inert" {
							return
						}
					}
					for _, attr := range node.Attr {
						if attr.Key == "data-calendar-config" || attr.Key == "data-map-config" {
							var config struct {
								Events []struct{ URL string }
								Points []struct{ Href string }
							}
							if err := json.Unmarshal([]byte(attr.Val), &config); err != nil {
								t.Fatal(err)
							}
							for _, point := range config.Points {
								if point.Href != "" {
									links++
								}
							}
							for _, event := range config.Events {
								if event.URL != "" {
									links++
								}
							}
						}
					}
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						walk(child)
					}
				}
				walk(root)
				if !disabled && links == 0 {
					t.Fatal("enabled view supplies no destination to its enhancement")
				}
				if disabled && links != 0 {
					t.Errorf("disabled view still supplies %d active destinations to its enhancement", links)
				}
			}
		})
	}
	if cases != 4 {
		t.Fatalf("tested %d examples, want the declared day/week/map fixtures", cases)
	}
}
