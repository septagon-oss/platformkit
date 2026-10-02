package forms_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

// wrapperClass is the class of the element a component marks with
// data-component: the field wrapper whose width is the control's measure.
func wrapperClass(t *testing.T, node g.Node) string {
	t.Helper()
	var out strings.Builder
	if err := node.Render(&out); err != nil {
		t.Fatal(err)
	}
	for _, n := range elements(t, out.String()) {
		if attribute(n, "data-component") != "" {
			return attribute(n, "class")
		}
	}
	t.Fatalf("no data-component wrapper in %s", out.String())
	return ""
}

// TestEveryGeneratedControlSpansTheFormsMeasure: a field that asks for nothing
// takes the frame's bounded measure, so the generated form has to ask for the
// full one on every control it draws, or one form shows two widths (the selects
// fell to 384px beside 1136px inputs on /app/task/tasks/new). The expected class
// is read from the component itself, so this pins the request, not a class list.
func TestEveryGeneratedControlSpansTheFormsMeasure(t *testing.T) {
	full := wrapperClass(t, components.Input(components.InputProps{Name: "x", FullWidth: true}))
	bounded := wrapperClass(t, components.Input(components.InputProps{Name: "x"}))
	if full == bounded {
		t.Fatalf("a full-width field and a bounded one wear the same class %q; nothing to tell apart", full)
	}
	for _, f := range []entity.Field{
		{Name: "title", Type: entity.TypeString},
		{Name: "description", Type: entity.TypeText, Widget: "textarea"},
		{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}},
		{Name: "priority", Type: entity.TypeString, Widget: "select", Enum: []string{"low", "high"}},
	} {
		got := wrapperClass(t, forms.Control(forms.ControlProps{Field: forms.Field{Definition: f, Label: f.Name}}))
		if got != full {
			t.Errorf("the generated control for %q wears %q, not the full measure %q", f.Name, got, full)
		}
	}
}
