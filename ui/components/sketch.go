package components

import (
	"cmp"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// SketchValue supplies ordered [x, y, elapsed milliseconds] tuples in a 0–1000
// square. Strokes must validate the whole drawing before returning any points;
// an error renders unavailable. Sketch only reads the returned slices.
type SketchValue interface {
	Strokes() ([][][3]int64, error)
}

// SketchProps describes a drawing at its caller's layout size. Label is required;
// nil Value is empty. Sentences default to English; invalid values render unavailable.
type SketchProps struct {
	ComponentProps
	Value           SketchValue
	Label           string
	EmptyText       string
	UnavailableText string
}

// Sketch renders validated strokes as a named SVG without changing their data.
// It reads no assets and uses identical geometry for thumbnails and full views.
func Sketch(p SketchProps) g.Node {
	attrs := append(baseAttrs(p.ComponentProps), classes("", p.Class), g.Attr("data-component", "sketch"))
	var strokes [][][3]int64
	var err error
	if p.Value != nil {
		strokes, err = p.Value.Strokes()
	}
	if strings.TrimSpace(p.Label) == "" || err != nil {
		return h.Div(append(attrs, g.Text(cmp.Or(p.UnavailableText, "Drawing unavailable.")))...)
	}
	shapes := []g.Node{g.Attr("viewBox", "0 0 1000 1000"), g.Attr("preserveAspectRatio", "xMidYMid meet"),
		g.Attr("role", "img"), g.Attr("aria-label", p.Label), g.Attr("fill", "none"), g.Attr("stroke", "currentColor"),
		g.Attr("stroke-width", "12"), g.Attr("stroke-linecap", "round"), g.Attr("stroke-linejoin", "round")}
	for _, stroke := range strokes {
		if len(stroke) == 1 {
			shapes = append(shapes, g.El("circle", g.Attr("cx", strconv.FormatInt(stroke[0][0], 10)), g.Attr("cy", strconv.FormatInt(stroke[0][1], 10)), g.Attr("r", "6"), g.Attr("fill", "currentColor"), g.Attr("stroke", "none")))
			continue
		}
		var points strings.Builder
		for i, point := range stroke {
			if i > 0 {
				points.WriteByte(' ')
			}
			points.WriteString(strconv.FormatInt(point[0], 10))
			points.WriteByte(',')
			points.WriteString(strconv.FormatInt(point[1], 10))
		}
		shapes = append(shapes, g.El("polyline", g.Attr("points", points.String())))
	}
	if len(strokes) == 0 {
		shapes = append(shapes, g.El("desc", g.Text(cmp.Or(p.EmptyText, "No drawing."))))
	}
	return h.Div(append(attrs, g.El("svg", shapes...))...)
}
