package components

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type MapPoint struct {
	ID          string  `json:"id"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	StatusKey   string  `json:"statusKey"`
	StatusText  string  `json:"statusText"`
	Href        string  `json:"href"`
}
type MapLegend struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Tone   string `json:"tone,omitempty"`
	Symbol string `json:"symbol" enum:"circle,square,triangle"`
}
type MapViewport struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Zoom      int     `json:"zoom"`
}
type Attribution struct {
	Text string `json:"text"`
	Href string `json:"href,omitempty"`
}
type MapTiles struct {
	URLTemplate  string        `json:"urlTemplate"`
	MinZoom      int           `json:"minZoom"`
	MaxZoom      int           `json:"maxZoom"`
	Attributions []Attribution `json:"attributions"`
}
type MapViewProps struct {
	ComponentProps
	Label              string            `json:"label"`
	State              ContentState      `json:"state,omitzero"`
	Points             []MapPoint        `json:"points,omitempty"`
	SelectedID         string            `json:"selectedID,omitempty"`
	View               string            `json:"view,omitempty" enum:",list,map"`
	Views              []ChoiceLink      `json:"views,omitempty"`
	MapLabel           string            `json:"mapLabel"`
	ListLabel          string            `json:"listLabel"`
	StatusLabel        string            `json:"statusLabel"`
	Legend             []MapLegend       `json:"legend,omitempty"`
	Viewport           MapViewport       `json:"viewport,omitzero"`
	Tiles              *MapTiles         `json:"tiles,omitempty"`
	MapUnavailableText string            `json:"mapUnavailableText"`
	ZoomInLabel        string            `json:"zoomInLabel"`
	ZoomOutLabel       string            `json:"zoomOutLabel"`
	SnapshotText       string            `json:"snapshotText"`
	Detail             *DetailSheetProps `json:"detail,omitempty"`
}
type MapViewSlots struct {
	StateSlots
	ListItem        func(MapPoint) g.Node
	SelectedContent func(MapPoint) g.Node
}

func coordinate(lat, lng float64) bool {
	return finite(lat) && finite(lng) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}
func (p MapViewProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Points) > 0 || p.SelectedID != "" || p.Tiles != nil || p.Detail != nil || p.Viewport.Latitude != 0 || p.Viewport.Longitude != 0 || p.Viewport.Zoom != 0); err != nil {
		return err
	}
	if !p.State.ready() {
		// A read that returned nothing keeps no route into what it returned last
		// time: the refusal this renderer writes names only the state, but the same
		// Props are captured as typed input, where a stale href would travel.
		if retainedNavigation(p.State.Status, p.Views) {
			return fmt.Errorf("MapView: absent content must clear navigation into the previous result")
		}
		return nil
	}
	if !required(p.ID, p.MapLabel, p.ListLabel, p.StatusLabel, p.MapUnavailableText, p.ZoomInLabel, p.ZoomOutLabel, p.SnapshotText) || !coordinate(p.Viewport.Latitude, p.Viewport.Longitude) || p.Viewport.Zoom < 0 || p.Viewport.Zoom > 24 {
		return fmt.Errorf("MapView: labels and bounded viewport required")
	}
	if p.View != "" && p.View != "list" && p.View != "map" {
		return fmt.Errorf("MapView: unknown view")
	}
	legends := map[string]bool{}
	for _, legend := range p.Legend {
		if !required(legend.Key, legend.Label) || legends[legend.Key] {
			return fmt.Errorf("MapView: unique legend keys and labels required")
		}
		legends[legend.Key] = true
		switch legend.Symbol {
		case "circle", "square", "triangle":
		default:
			return fmt.Errorf("MapView: unknown legend symbol")
		}
	}
	ids := map[string]bool{}
	for _, point := range p.Points {
		if !required(point.ID, point.Title, point.StatusText, point.Href) || ids[point.ID] || !legends[point.StatusKey] || !coordinate(point.Latitude, point.Longitude) {
			return fmt.Errorf("MapView: unique labeled points, valid coordinates and legend membership required")
		}
		ids[point.ID] = true
	}
	if p.SelectedID != "" && !ids[p.SelectedID] {
		return fmt.Errorf("MapView: selection must match a supplied point")
	}
	if p.Tiles != nil {
		tile := p.Tiles
		parsed, err := url.Parse(tile.URLTemplate)
		if err != nil || !strings.HasPrefix(tile.URLTemplate, "/") || strings.HasPrefix(tile.URLTemplate, "//") || strings.Contains(tile.URLTemplate, "\\") || parsed.Host != "" || parsed.Fragment != "" || tile.MinZoom < 0 || tile.MaxZoom > 24 || tile.MaxZoom < tile.MinZoom || p.Viewport.Zoom < tile.MinZoom || p.Viewport.Zoom > tile.MaxZoom {
			return fmt.Errorf("MapView: same-origin tiles and valid zoom bounds required")
		}
		for _, part := range []string{"{z}", "{x}", "{y}"} {
			if strings.Count(tile.URLTemplate, part) != 1 {
				return fmt.Errorf("MapView: tile URL needs each coordinate placeholder once")
			}
		}
		if len(tile.Attributions) == 0 {
			return fmt.Errorf("MapView: tile attribution required")
		}
		for _, a := range tile.Attributions {
			if !required(a.Text) {
				return fmt.Errorf("MapView: empty attribution")
			}
		}
	}
	if p.Detail != nil {
		if p.SelectedID == "" || !ids[p.SelectedID] || p.Detail.State.ready() && p.Detail.ItemID != p.SelectedID {
			return fmt.Errorf("MapView: detail must match selected authorized point")
		}
		detail := *p.Detail
		detail.ID = p.ID + "-detail"
		if err := detail.Validate(); err != nil {
			return err
		}
	}
	return validateChoices(p.Views)
}
func MapView(p MapViewProps) g.Node { return MapViewWithSlots(p, MapViewSlots{}) }
func MapViewWithSlots(p MapViewProps, slots MapViewSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "map-view", p.Label, stateBody(p.State, slots.StateSlots))
	}
	views := slices.Clone(p.Views)
	for i := range views {
		views[i].Selected = views[i].Key == p.View || p.View == "" && views[i].Key == "list"
	}
	var points []DataRow
	byID := map[string]MapPoint{}
	for _, point := range p.Points {
		byID[point.ID] = point
		points = append(points, DataRow{TableRow: TableRow{ID: point.ID, Cells: map[string]any{"title": point.Title, "status": point.StatusText}}, Href: point.Href})
	}
	var list g.Node
	if len(points) > 0 {
		list = DataListWithSlots(DataListProps{Label: p.ListLabel, Columns: []TableColumn{{Key: "title", Label: p.ListLabel, Primary: true}, {Key: "status", Label: p.StatusLabel}}, Groups: []DataGroup{{Key: "points", Rows: points}}}, DataListSlots{TableSlots: TableSlots{Cell: func(row TableRow, col TableColumn) g.Node {
			if col.Primary {
				if slots.ListItem != nil {
					return slots.ListItem(byID[row.ID])
				}
				point := byID[row.ID]
				return Stack(StackProps{Gap: "1"}, recoveryAction(ButtonProps{Label: point.Title, Href: point.Href, Variant: "link"}, p.Disabled), Text(TextProps{Content: point.Description, Size: "sm"}))
			}
			return nil
		}, RowAttrs: func(row TableRow) []g.Node {
			if row.ID == p.SelectedID {
				return []g.Node{g.Attr("aria-current", "true")}
			}
			return nil
		}}})
	}
	var legend []g.Node
	for _, item := range p.Legend {
		symbol := map[string]string{"circle": "●", "square": "■", "triangle": "▲"}[item.Symbol]
		legend = append(legend, Flex(FlexProps{Gap: "2", Align: "center"}, h.Span(g.Attr("aria-hidden", "true"), g.Text(symbol)), Badge(BadgeProps{Label: item.Label, Tone: item.Tone})))
	}
	var mapNode, detail g.Node
	if p.View == "map" {
		// A disabled view hands its engine nothing to navigate to; the copy keeps the caller's slice whole.
		points := slices.Clone(p.Points)
		if p.Disabled {
			for i := range points {
				points[i].Href = ""
			}
		}
		config, _ := json.Marshal(map[string]any{"points": points, "legend": p.Legend, "viewport": p.Viewport, "tiles": p.Tiles, "selected": p.SelectedID, "zoomIn": p.ZoomInLabel, "zoomOut": p.ZoomOutLabel})
		mapNode = h.Div(h.Div(g.Attr("data-map-config", string(config)), g.Attr("data-map-engine", ""), h.Role("region"), g.Attr("aria-label", p.MapLabel), g.Attr("tabindex", "0")), h.P(g.Attr("data-map-fallback", ""), g.Text(p.MapUnavailableText)))
	}
	if p.Detail != nil {
		sheet := *p.Detail
		sheet.ID = p.ID + "-detail"
		var body []g.Node
		if sheet.State.ready() && slots.SelectedContent != nil {
			body = []g.Node{slots.SelectedContent(byID[p.SelectedID])}
		}
		detail = DetailSheetWithSlots(sheet, DetailPanelSlots{Body: body})
	}
	var attributions []g.Node
	if p.Tiles != nil {
		for _, a := range p.Tiles.Attributions {
			var text g.Node = g.Text(a.Text)
			if a.Href != "" {
				text = Link(LinkProps{Label: a.Text, Href: a.Href})
			}
			attributions = append(attributions, text)
		}
	}
	return sharedSection(p.ComponentProps, "map-view", p.Label, stateBody(p.State, slots.StateSlots), choiceLinks(views, p.Disabled), Text(TextProps{Content: p.SnapshotText, Size: "sm"}), Flex(FlexProps{Wrap: true, Gap: "3"}, legend...), mapNode, Flex(FlexProps{Wrap: true, Gap: "2"}, attributions...), list, detail)
}
