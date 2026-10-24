package components

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// SketchProps describes a drawing at its caller's layout size. Label is required;
// empty state sentences default to English. Invalid values render unavailable.
type SketchProps struct {
	ComponentProps
	Value           entity.Sketch
	Label           string
	EmptyText       string
	UnavailableText string
}

// Sketch renders validated strokes as a named SVG without changing their data.
// It reads no assets and uses identical geometry for thumbnails and full views.
func Sketch(p SketchProps) g.Node {
	attrs := append(baseAttrs(p.ComponentProps), classes("", p.Class), g.Attr("data-component", "sketch"))
	if strings.TrimSpace(p.Label) == "" || p.Value.Validate() != nil {
		return h.Div(append(attrs, g.Text(cmp.Or(p.UnavailableText, "Drawing unavailable.")))...)
	}
	shapes := []g.Node{g.Attr("viewBox", "0 0 1000 1000"), g.Attr("preserveAspectRatio", "xMidYMid meet"),
		g.Attr("role", "img"), g.Attr("aria-label", p.Label), g.Attr("fill", "none"), g.Attr("stroke", "currentColor"),
		g.Attr("stroke-width", "12"), g.Attr("stroke-linecap", "round"), g.Attr("stroke-linejoin", "round")}
	for _, stroke := range p.Value {
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
	if len(p.Value) == 0 {
		shapes = append(shapes, g.El("desc", g.Text(cmp.Or(p.EmptyText, "No drawing."))))
	}
	return h.Div(append(attrs, g.El("svg", shapes...))...)
}
