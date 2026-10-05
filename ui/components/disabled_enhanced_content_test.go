package components_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	nethtml "golang.org/x/net/html"
)

// engineConfig returns every calendar or map engine payload a rendered node hands its enhancement.
func engineConfig(t *testing.T, html string) (events []map[string]any, points []map[string]any) {
	t.Helper()
	root, err := nethtml.Parse(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		for _, attr := range node.Attr {
			if attr.Key == "data-calendar-config" || attr.Key == "data-map-config" {
				var config struct {
					Events []map[string]any
					Points []map[string]any
				}
				if err := json.Unmarshal([]byte(attr.Val), &config); err != nil {
					t.Fatal(err)
				}
				events = append(events, config.Events...)
				points = append(points, config.Points...)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return events, points
}

// Disabled removes destinations, never content: a disabled engine still receives every event and point the
// enabled one does, under the same titles.
func TestDisabledEnhancedViewsKeepTheirContent(t *testing.T) {
	cases := 0
	for _, example := range examples.Gallery() {
		if example.ID != "pk-ui.component.calendar/day-en" && example.ID != "pk-ui.component.calendar/week-pt-PT" &&
			example.ID != "pk-ui.component.map-view/map-en" && example.ID != "pk-ui.component.map-view/map-pt-PT" {
			continue
		}
		cases++
		t.Run(example.ID, func(t *testing.T) {
			titles := map[bool][]string{}
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
				events, points := engineConfig(t, out.String())
				for _, item := range append(events, points...) {
					title, _ := item["title"].(string)
					titles[disabled] = append(titles[disabled], title)
				}
			}
			if len(titles[false]) == 0 {
				t.Fatal("enabled view hands its enhancement nothing to show")
			}
			if strings.Join(titles[true], "\x00") != strings.Join(titles[false], "\x00") {
				t.Fatalf("disabled view shows %q, enabled view shows %q", titles[true], titles[false])
			}
		})
	}
	if cases != 4 {
		t.Fatalf("tested %d examples, want the declared day/week/map fixtures", cases)
	}
}

// Rendering a disabled map must not take the destinations out of the caller's own points: the same props,
// rendered enabled afterwards, still link every point.
func TestDisabledMapLeavesTheCallersPointsWhole(t *testing.T) {
	props := c.MapViewProps{
		ComponentProps:     c.ComponentProps{ID: "locations"},
		Label:              "Locations",
		View:               "map",
		MapLabel:           "Map of locations",
		ListLabel:          "Location list",
		StatusLabel:        "Status",
		MapUnavailableText: "Map unavailable",
		ZoomInLabel:        "Zoom in",
		ZoomOutLabel:       "Zoom out",
		SnapshotText:       "Current locations",
		Viewport:           c.MapViewport{Zoom: 1},
		Legend:             []c.MapLegend{{Key: "open", Label: "Open", Symbol: "circle"}},
		Points: []c.MapPoint{
			{ID: "one", Title: "First place", StatusKey: "open", StatusText: "Open", Href: "/places/one"},
			{ID: "two", Title: "Second place", StatusKey: "open", StatusText: "Open", Href: "/places/two"},
		},
	}
	disabled := props
	disabled.Disabled = true
	var off strings.Builder
	if err := c.MapView(disabled).Render(&off); err != nil {
		t.Fatal(err)
	}
	if _, points := engineConfig(t, off.String()); len(points) != 2 {
		t.Fatalf("disabled map hands its engine %d points, want 2", len(points))
	}
	if props.Points[0].Href != "/places/one" || props.Points[1].Href != "/places/two" {
		t.Fatalf("disabled render rewrote the caller's points: %+v", props.Points)
	}
	var on strings.Builder
	if err := c.MapView(props).Render(&on); err != nil {
		t.Fatal(err)
	}
	_, points := engineConfig(t, on.String())
	for _, point := range points {
		if href, _ := point["href"].(string); href == "" {
			t.Fatalf("enabled map after a disabled render lost a destination: %v", point)
		}
	}
	if len(points) != 2 {
		t.Fatalf("enabled map hands its engine %d points, want 2", len(points))
	}
}
