package forms_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// controlOf renders one field's control and reports what a browser received.
// It goes through Control rather than a component constructor, because the
// claim under test is that a schema's ui:"widget:…" directive reaches a page.
func controlOf(t *testing.T, f entity.Field) (tag, typeAttr, markup string) {
	t.Helper()
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: f, Label: "Cover"},
	}).Render(&out); err != nil {
		t.Fatal(err)
	}
	for _, node := range elements(t, out.String()) {
		if slices.Contains([]string{"input", "select", "textarea"}, node.Data) {
			return node.Data, attribute(node, "type"), out.String()
		}
	}
	t.Fatalf("no control rendered for %q: %s", f.Name, out.String())
	return "", "", ""
}

// drawnWidgets names the control each admitted widget must produce: the element
// and, for an input, its type. entity-picker is the one widget that draws the
// plain text control and says so in its help text, because no picker exists yet.
var drawnWidgets = map[string]struct{ tag, typeAttr string }{
	"checkbox":      {"input", "checkbox"},
	"color":         {"input", "color"},
	"date":          {"input", "date"},
	"datetime":      {"input", "datetime-local"},
	"email":         {"input", "email"},
	"entity-picker": {"input", "text"},
	"file":          {"input", "file"},
	"hidden":        {"input", "hidden"},
	"month":         {"input", "month"},
	"number":        {"input", "number"},
	"password":      {"input", "password"},
	"search":        {"input", "search"},
	"select":        {"select", ""},
	"tel":           {"input", "tel"},
	"text":          {"input", "text"},
	"textarea":      {"textarea", ""},
	"time":          {"input", "time"},
	"url":           {"input", "url"},
	"week":          {"input", "week"},
}

// TestEveryAdmittedWidgetNamesADrawingTest.
//
// entity.Widgets is the vocabulary a struct tag may name; this table is the
// control each name draws. They must name the same set, because the alternative
// is what shipped: a name a schema could write and no renderer had heard of,
// which drew a text input and said nothing. The E6 review added a file input to
// the component; this is the check that would have noticed it reached no form.
func TestEveryAdmittedWidgetNamesADrawingTest(t *testing.T) {
	for _, widget := range entity.Widgets {
		if _, ok := drawnWidgets[widget]; !ok {
			t.Errorf("entity.Widgets admits %q and nothing here says what it draws", widget)
		}
	}
	admitted, drawn := slices.Clone(entity.Widgets), mapsKeys(drawnWidgets)
	slices.Sort(admitted)
	slices.Sort(drawn)
	if !slices.Equal(admitted, drawn) {
		t.Errorf("the admitted vocabulary and the drawn vocabulary differ:\n  admitted %v\n  drawn    %v", admitted, drawn)
	}
}

func TestAWidgetDrawsTheControlItNames(t *testing.T) {
	t.Parallel()
	for _, widget := range entity.Widgets {
		want := drawnWidgets[widget]
		t.Run(widget, func(t *testing.T) {
			tag, got, markup := controlOf(t, entity.Field{Name: "field", Type: entity.TypeString, Widget: widget})
			if tag != want.tag || got != want.typeAttr {
				t.Errorf("widget %q drew <%s type=%q>, want <%s type=%q>\n%s",
					widget, tag, got, want.tag, want.typeAttr, markup)
			}
			if widget == "file" && strings.Contains(markup, `value=`) {
				t.Errorf("a file input carries a value: %s", markup)
			}
			if widget == "entity-picker" && !strings.Contains(markup, "no picker") {
				t.Errorf("entity-picker promises a picker it does not have: %s", markup)
			}
		})
	}
}

// TestTheWidgetsTheModulesAlreadyNameKeepTheirControls is the regression half of
// the change. `datetime` and `checkbox` sit on fifteen fields of the
// foundation's own entities, where the Go type picked the control and the widget
// name was ignored. They are now drawn because the name says so, and the DOM
// must not move: the browser journeys and an operator's saved view read these
// elements.
func TestTheWidgetsTheModulesAlreadyNameKeepTheirControls(t *testing.T) {
	t.Parallel()
	for _, f := range []entity.Field{
		{Name: "due_at", Type: entity.TypeTime, Widget: "datetime"},
		{Name: "settled_at", Type: entity.TypeString, Widget: "datetime"},
	} {
		tag, got, markup := controlOf(t, f)
		if tag != "input" || got != "datetime-local" {
			t.Errorf("%s (%s/%s) drew <%s type=%q>: %s", f.Name, f.Type, f.Widget, tag, got, markup)
		}
	}
	for _, f := range []entity.Field{
		{Name: "active", Type: entity.TypeBool, Widget: "checkbox"},
		{Name: "sla_breached", Type: entity.TypeString, Widget: "checkbox"},
	} {
		tag, got, markup := controlOf(t, f)
		if tag != "input" || got != "checkbox" {
			t.Errorf("%s (%s/%s) drew <%s type=%q>: %s", f.Name, f.Type, f.Widget, tag, got, markup)
		}
	}
}

// TestAFormCarriesTheEncodingItsControlsNeed — a file control submits a body no
// urlencoded form can carry. The reference app could not render one at all: the
// component accepted Type "file" and no schema could ask for it.
func TestAFormCarriesTheEncodingItsControlsNeed(t *testing.T) {
	t.Parallel()
	title := forms.Field{Definition: entity.Field{Name: "title", Type: entity.TypeString}, Label: "Title"}
	cover := forms.Field{Definition: entity.Field{Name: "cover", Type: entity.TypeString, Widget: "file"}, Label: "Cover"}
	for name, test := range map[string]struct {
		model   forms.Model
		enctype string
	}{
		"a file control": {forms.Model{Fields: []forms.Field{title, cover}}, "multipart/form-data"},
		"no file":        {forms.Model{Fields: []forms.Field{title}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			example, err := forms.Example("form", test.model, forms.Options{Namespace: "form", Action: "/save"})
			if err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			if err := example.Node.Render(&out); err != nil {
				t.Fatal(err)
			}
			got := ""
			for _, node := range elements(t, out.String()) {
				if node.Data == "form" {
					got = attribute(node, "enctype")
				}
			}
			if got != test.enctype {
				t.Errorf("the form carries enctype %q, want %q\n%s", got, test.enctype, out.String())
			}
		})
	}
}

func mapsKeys(m map[string]struct{ tag, typeAttr string }) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
