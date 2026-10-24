package components_test

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"strings"
	"testing"
)

func TestSketchSVGKeepsGeometryAndAccessibleName(t *testing.T) {
	value := entity.Sketch{{{0, 0, 0}, {1000, 1000, 40}}, {{500, 250, 0}}}
	var geometry string
	for _, size := range []string{"64", "640"} {
		out := renderMedia(t, components.Sketch(components.SketchProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-size": size}}, Value: value, Label: `Drawing "<one>"`}))
		for _, want := range []string{`viewBox="0 0 1000 1000"`, `points="0,0 1000,1000"`, `cx="500"`, `cy="250"`, `r="6"`, `role="img"`, `aria-label="Drawing &#34;&lt;one&gt;&#34;"`} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %s: %s", want, out)
			}
		}
		svg := out[strings.Index(out, "<svg"):]
		if geometry != "" && svg != geometry {
			t.Fatal("size changed geometry")
		}
		geometry = svg
	}
	for _, value := range []entity.Sketch{nil, {}} {
		out := renderMedia(t, components.Sketch(components.SketchProps{Value: value, Label: "Drawing"}))
		if !strings.Contains(out, "No drawing.") || strings.Contains(out, "polyline") {
			t.Fatal(out)
		}
	}
	if out := renderMedia(t, components.Sketch(components.SketchProps{Label: "Drawing"})); !strings.Contains(out, "No drawing.") || !strings.Contains(out, "<svg") {
		t.Fatal(out)
	}
	for _, p := range []components.SketchProps{{Value: entity.Sketch{{}}, Label: "Drawing"}, {Value: value}} {
		out := renderMedia(t, components.Sketch(p))
		if !strings.Contains(out, "Drawing unavailable.") || strings.Contains(out, "<svg") {
			t.Fatal(out)
		}
	}
}
