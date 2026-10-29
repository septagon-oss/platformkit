package resource

import (
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func formValueString(fields []entity.Field, name string, value any) string {
	if f, ok := entity.FieldNamed(fields, name); ok && f.Type == entity.TypeSketch {
		if raw, ok := value.(string); ok {
			return raw
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return "invalid"
		}
		return string(encoded)
	}
	return display.Text(value)
}

func sketchReading(value any, label, size string, text func(string, string) string) g.Node {
	if text == nil {
		text = func(_, fallback string) string { return fallback }
	}
	raw := formValueString([]entity.Field{{Name: "value", Type: entity.TypeSketch}}, "value", value)
	var drawing entity.Sketch
	if err := drawing.UnmarshalJSON([]byte(raw)); err != nil {
		drawing = entity.Sketch{{}}
	}
	return components.Sketch(components.SketchProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sketch-size": size}},
		Value: drawing, Label: label, EmptyText: text("forms.sketch.empty", "No drawing."), UnavailableText: text("forms.sketch.unavailable", "Drawing unavailable.")})
}

func drawingCell(f entity.Field, value any, text string) any {
	if f.Type == entity.TypeSketch {
		return value
	}
	return text
}

func formModules(r Resource) []string {
	modules := richtextModules(r)
	for _, f := range r.Schema.Fields {
		if f.Type == entity.TypeSketch && (r.field(f.Name).Editable.Create || r.field(f.Name).Editable.Update) {
			return append(modules, "js/sketch.js")
		}
	}
	return modules
}

func sketchCommandModules(r Resource) []string {
	for _, c := range r.offeredCommands() {
		for _, f := range c.Fields {
			if f.Type == entity.TypeSketch {
				return []string{"js/sketch.js"}
			}
		}
	}
	return nil
}
