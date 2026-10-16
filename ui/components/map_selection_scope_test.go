package components_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func TestMapSelectionStaysWithinSuppliedPoints(t *testing.T) {
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
		Viewport:           c.MapViewport{Latitude: 0, Longitude: 0, Zoom: 1},
		Legend:             []c.MapLegend{{Key: "open", Label: "Open", Symbol: "circle"}},
		Points: []c.MapPoint{{ID: "authorized-point", Title: "Open place", StatusKey: "open",
			StatusText: "Open", Href: "/places/authorized-point"}},
	}
	var valid strings.Builder
	if err := c.MapView(props).Render(&valid); err != nil || !strings.Contains(valid.String(), "authorized-point") {
		t.Fatalf("a supplied point must render before checking an invalid selection: %v", err)
	}

	props.SelectedID = "other-tenant-point"
	var out strings.Builder
	err := c.MapView(props).Render(&out)
	if err != nil && out.Len() != 0 {
		t.Fatalf("invalid selection wrote partial output: %q, %v", out.String(), err)
	}
	if strings.Contains(out.String(), "other-tenant-point") {
		t.Fatal("map serialized a selection outside the supplied point set")
	}
}
