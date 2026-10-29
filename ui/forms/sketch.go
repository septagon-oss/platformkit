package forms

import (
	"cmp"
	"strconv"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func sketchControl(p ControlProps) g.Node {
	f := p.Field.Definition
	label := cmp.Or(p.Field.Label, f.Presentation.Label, f.Name)
	word := func(key, fallback string) string {
		if p.Text != nil {
			return p.Text("forms.sketch."+key, fallback)
		}
		return fallback
	}
	raw := p.Value
	if raw == "" && !p.Field.Editor.Submitted {
		raw = "null"
	}
	var value entity.Sketch
	err := value.UnmarshalJSON([]byte(raw))
	preview := components.Sketch(components.SketchProps{Value: value, Label: label,
		EmptyText: word("empty", "No drawing."), UnavailableText: word("unavailable", "Drawing unavailable.")})
	if err != nil {
		preview = h.P(g.Text(word("unavailable", "Drawing unavailable.")))
	}
	if p.Immutable || entity.ServerOwned(f) {
		return h.Div(preview, h.P(g.Text(immutableNote(true))))
	}
	id := cmp.Or(p.ID, f.Name)
	described := id + "-help"
	if p.Error != "" {
		described += " " + id + "-error"
	}
	attrs := []g.Node{h.ID(id + "-group"), g.Attr("data-sketch", ""), g.Attr("tabindex", "-1"),
		g.Attr("data-sketch-strokes", strconv.Itoa(entity.SketchMaxStrokes)), g.Attr("data-sketch-points", strconv.Itoa(entity.SketchMaxPoints)),
		g.Attr("data-sketch-coordinate", strconv.Itoa(entity.SketchMaxCoordinate)), g.Attr("data-sketch-milliseconds", strconv.FormatInt(entity.SketchMaxMilliseconds, 10))}
	for _, line := range sketchSentences {
		attrs = append(attrs, g.Attr("data-sketch-"+line.key, word(line.key, line.text)))
	}
	board := []g.Node{h.ID(id), g.Attr("tabindex", "0"), h.Role("group"), h.Aria("labelledby", id+"-label"), h.Aria("describedby", described), h.Hidden("")}
	if p.Error != "" {
		board = append(board, h.Aria("invalid", "true"))
		attrs = append(attrs, h.Aria("invalid", "true"))
	}
	button := func(action, text string) g.Node {
		return components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sketch-action": action}}, Type: "button", Variant: "secondary", Label: word(action, text)})
	}
	help := word("help", "Draw with a pointer, or use arrow keys to move, Shift for fine steps, Space or Enter to start and finish, and Escape to cancel.")
	if f.Required {
		help += " " + word("required", "Add a drawing before continuing.")
	}
	attrs = append(attrs, h.Legend(h.ID(id+"-label"), g.Text(label)), h.Input(h.Type("hidden"), h.Name(f.Name), h.Value(raw), g.Attr("data-sketch-value", "")),
		h.Div(g.Attr("data-sketch-preview", ""), preview), g.El("canvas", board...),
		h.P(h.ID(id+"-help"), g.Text(hint(p.Field.Help, help))), h.P(h.ID(id+"-error"), g.Text(p.Error)),
		h.P(g.Attr("data-sketch-fallback", ""), g.Text(word("fallback", "Drawing needs JavaScript. Your saved drawing will be kept."))),
		h.Div(g.Attr("data-sketch-actions", ""), h.Hidden(""), button("undo", "Undo"), button("clear", "Clear")),
		h.P(g.Attr("data-sketch-status", ""), h.Role("status")))
	return h.FieldSet(attrs...)
}

var sketchSentences = []struct{ key, text string }{
	{"unavailable", "Drawing unavailable."}, {"started", "Stroke started."}, {"finished", "Stroke added."},
	{"canceled", "Stroke canceled."}, {"undone", "Last change undone."}, {"cleared", "Drawing cleared."},
	{"stroke-limit", "A drawing can contain at most 400 strokes. Undo or clear some strokes and try again."},
	{"point-limit", "A drawing can contain at most 20,000 points. Undo or clear some strokes and try again."},
	{"time-limit", "Use elapsed milliseconds between 0 and 9007199254740991."},
}
