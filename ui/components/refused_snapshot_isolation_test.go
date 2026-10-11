package components_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func TestRefusedSharedViewsDiscardPriorResultMarkup(t *testing.T) {
	const private = "tenant-one-private-record"
	refused := c.ContentState{Status: c.MediaRefused, Title: "Acesso recusado", Text: "Sem acesso aos registos."}
	render := func(t *testing.T, node g.Node) (string, error) {
		t.Helper()
		var out strings.Builder
		err := node.Render(&out)
		return out.String(), err
	}
	check := func(t *testing.T, ready g.Node, stale g.Node, cleared g.Node) {
		t.Helper()
		if html, err := render(t, ready); err != nil || !strings.Contains(html, private) {
			t.Fatalf("authorized snapshot was not rendered: %v, %q", err, html)
		}
		if html, err := render(t, stale); err == nil || html != "" {
			t.Fatalf("retained snapshot must be refused before output: %v, %q", err, html)
		}
		if html, err := render(t, cleared); err != nil || !strings.Contains(html, refused.Text) || strings.Contains(html, private) {
			t.Fatalf("cleared refusal must speak its supplied language without old data: %v, %q", err, html)
		}
	}

	list := c.DataListProps{Label: "Records", Columns: []c.TableColumn{{Key: "title", Label: "Title", Primary: true}},
		Groups: []c.DataGroup{{Key: "records", Rows: []c.DataRow{{TableRow: c.TableRow{ID: private, Cells: map[string]any{"title": private}}}}}}}
	staleList := list
	staleList.State = refused
	clearList := c.DataListProps{Label: "Records", State: refused}
	t.Run("data list", func(t *testing.T) { check(t, c.DataList(list), c.DataList(staleList), c.DataList(clearList)) })

	mapView := c.MapViewProps{ComponentProps: c.ComponentProps{ID: "places"}, Label: "Places", View: "map",
		MapLabel: "Places map", ListLabel: "Places list", StatusLabel: "Status", MapUnavailableText: "Map unavailable",
		ZoomInLabel: "Zoom in", ZoomOutLabel: "Zoom out", SnapshotText: "Current places",
		Viewport: c.MapViewport{Latitude: 1, Longitude: 1, Zoom: 2},
		Legend:   []c.MapLegend{{Key: "open", Label: "Open", Symbol: "circle"}},
		Points:   []c.MapPoint{{ID: private, Title: private, StatusKey: "open", StatusText: "Open", Href: "/places/one"}}}
	staleMap := mapView
	staleMap.State = refused
	clearMap := mapView
	clearMap.State, clearMap.Points, clearMap.Viewport = refused, nil, c.MapViewport{}
	t.Run("map", func(t *testing.T) { check(t, c.MapView(mapView), c.MapView(staleMap), c.MapView(clearMap)) })
}
