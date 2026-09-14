package forms_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/forms"
	"golang.org/x/net/html"
)

func TestAmbiguousFieldIdentitiesAreRefusedBeforeRendering(t *testing.T) {
	for name, fields := range map[string][]forms.Field{
		"empty": {{Definition: entity.Field{Type: entity.TypeString}}},
		"duplicate": {
			{Definition: entity.Field{Name: "answer", Type: entity.TypeString}},
			{Definition: entity.Field{Name: "answer", Type: entity.TypeBool}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			example, err := forms.Example("editor", forms.Model{Fields: fields}, forms.Options{Namespace: "editor"})
			if !errors.Is(err, forms.ErrFields) || example.Node != nil {
				t.Fatalf("ambiguous identities returned a renderable form: %v", err)
			}
		})
	}
}

func fixture() forms.Model {
	return forms.Model{Fields: []forms.Field{
		{Definition: entity.Field{Name: "id", ReadOnly: true}},
		{Definition: entity.Field{Name: "title", Type: entity.TypeString, Required: true, Default: "Untitled", Doc: "A useful name."}, Label: "Title"},
		{Definition: entity.Field{Name: "body", Type: entity.TypeText, Widget: "textarea", Required: true, Doc: "Describe it."}, Label: "Description"},
		{Definition: entity.Field{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}, Default: "open", Required: true, Doc: "Current state."},
			Label: "Status", Options: []components.SelectOption{{Label: "Open", Value: "open"}, {Label: "Done", Value: "done"}}},
		{Definition: entity.Field{Name: "accepted", Type: entity.TypeBool, Required: true, Doc: "Read before accepting."}, Label: "I accept"},
		{Definition: entity.Field{Name: "when", Type: entity.TypeTime}, Label: "When"},
		{Definition: entity.Field{Name: "rank", Type: entity.TypeInt}, Label: "Rank"},
		{Definition: entity.Field{Name: "ratio", Type: entity.TypeFloat}, Label: "Ratio"},
		{Definition: entity.Field{Name: "title-error", Type: entity.TypeString}, Label: "Separate field"},
		{Definition: entity.Field{Name: "a/b c", Type: entity.TypeString}, Label: "Unusual JSON name"},
	}, Values: map[string]string{"title": "", "body": "Draft text", "status": "done", "accepted": "true", "when": "2026-09-13T12:34:56Z"},
		Errors: map[string]string{"title": "Choose a title", "body": "Explain more", "status": "Choose another state", "accepted": "Check this choice"}}
}

func describe(t *testing.T, id, namespace string, model forms.Model) (examples.Example, examples.ExampleDescription) {
	t.Helper()
	example, err := forms.Example(id, model, forms.Options{Namespace: namespace, Action: "/save", CancelURL: "/cancel", Title: "Edit record"})
	if err != nil {
		t.Fatal(err)
	}
	description, err := example.Describe()
	if err != nil {
		t.Fatal(err)
	}
	return example, description
}

func elements(t *testing.T, source string) []*html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	var nodes []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			nodes = append(nodes, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return nodes
}

func attribute(node *html.Node, name string) string {
	for _, value := range node.Attr {
		if value.Key == name {
			return value.Val
		}
	}
	return ""
}

func TestInstancesKeepControlsLabelsAndFeedbackSeparate(t *testing.T) {
	t.Parallel()
	model := fixture()
	first, one := describe(t, "source/first", "first-editor", model)
	second, two := describe(t, "source/second", "second_editor", model)
	nodes := elements(t, one.HTML+two.HTML)
	ids := map[string]bool{}
	for _, node := range nodes {
		if id := attribute(node, "id"); id != "" {
			if ids[id] {
				t.Fatalf("two form instances share DOM ID %q", id)
			}
			ids[id] = true
		}
	}
	controls := 0
	for _, node := range nodes {
		if node.Data == "form" {
			id := attribute(node, "id")
			if attribute(node, "hx-target") != "#"+id || attribute(node, "hx-select") != "#"+id {
				t.Fatalf("form targets another instance: %s", id)
			}
		}
		if !slices.Contains([]string{"input", "select", "textarea"}, node.Data) {
			continue
		}
		controls++
		id, name := attribute(node, "id"), attribute(node, "name")
		if id == "" || !slices.ContainsFunc(nodes, func(label *html.Node) bool {
			return label.Data == "label" && attribute(label, "for") == id
		}) {
			t.Fatalf("%s lacks its own native label", name)
		}
		if model.Errors[name] != "" {
			helpID := id + "-help"
			if node.Data == "textarea" {
				helpID = id + "-helper"
			}
			if attribute(node, "aria-invalid") != "true" || attribute(node, "aria-describedby") != id+"-error "+helpID {
				t.Fatalf("%s lost its own inline error/help relationship", name)
			}
			if !ids[id+"-error"] || !ids[helpID] {
				t.Fatalf("%s references nonexistent feedback", name)
			}
		}
	}
	if controls != 18 {
		t.Fatalf("rendered %d controls, want nine in each form", controls)
	}
	if _, err := ui.Export(design.Default(), append(examples.Gallery(), first, second)); err != nil {
		t.Fatalf("forms disagree with Core's captured interfaces: %v", err)
	}
}

func TestDefaultsImmutabilityAndNativeValues(t *testing.T) {
	t.Parallel()
	model := fixture()
	model.Create, model.Immutable = true, []string{"status"}
	_, description := describe(t, "source/new", "new-editor", model)
	if strings.Contains(description.HTML, `name="status"`) || strings.Contains(description.HTML, `name="id"`) || strings.Contains(description.HTML, `value="Untitled"`) {
		t.Fatal("create must hide owned fields and retain an explicitly empty input")
	}
	delete(model.Values, "title")
	_, defaults := describe(t, "source/defaults", "defaults", model)
	if !strings.Contains(defaults.HTML, `value="Untitled"`) || !strings.Contains(defaults.HTML, `value="2026-09-13T12:34"`) {
		t.Fatal("missing create values or time formatting changed")
	}
	model.Create = false
	_, edit := describe(t, "source/edit", "edit-editor", model)
	for _, node := range elements(t, edit.HTML) {
		if attribute(node, "name") == "status" && !slices.Contains(node.Attr, html.Attribute{Key: "disabled"}) {
			t.Fatal("immutable select must be natively disabled on edit")
		}
		if attribute(node, "name") == "accepted" {
			for _, name := range []string{"checked", "required"} {
				if !slices.Contains(node.Attr, html.Attribute{Key: name}) {
					t.Fatalf("boolean lost its native %s state", name)
				}
			}
		}
	}
}

func TestCapturedFormsKeepIdentityAndInputsThroughChanges(t *testing.T) {
	t.Parallel()
	model := fixture()
	form, before := describe(t, "source/form", "editor", model)
	slices.Reverse(model.Fields)
	_, reordered := describe(t, form.ID, "editor", model)
	for _, child := range before.Children {
		index := slices.IndexFunc(reordered.Children, func(other examples.ChildOccurrence) bool { return other.Description.ID == child.Description.ID })
		if index < 0 || !reflect.DeepEqual(child.Description, reordered.Children[index].Description) {
			t.Fatalf("reordering changed field identity or inputs: %s", child.Description.ID)
		}
	}
	model.Values["body"], model.Errors["body"] = "Changed", "Changed"
	for i := range model.Fields {
		if len(model.Fields[i].Options) > 0 {
			model.Fields[i].Options[0].Label = "Changed"
		}
	}
	again, err := form.Describe()
	if err != nil || !reflect.DeepEqual(before, again) {
		t.Fatalf("caller changes mutated the captured form: %v", err)
	}
	source := []examples.Example{form}
	base, err := ui.Export(design.Default(), source)
	if err != nil {
		t.Fatal(err)
	}
	edited, changed, err := ui.ProjectProps(design.Default(), source, ui.PropsProposal{
		BaseSHA256: base.SHA256, Path: []string{form.ID, "field/body"}, Props: json.RawMessage(`{"value":"Reviewed"}`)})
	if err != nil || !strings.Contains(changed.Examples[0].HTML, "Reviewed") {
		t.Fatalf("field no longer supports existing source edits: %v", err)
	}
	if _, _, err := ui.ProjectProps(design.Default(), edited, ui.PropsProposal{
		BaseSHA256: base.SHA256, Path: []string{form.ID, "field/body"}, Props: json.RawMessage(`{"value":"Stale"}`)}); !errors.Is(err, ui.ErrStaleExport) {
		t.Fatalf("stale form proposal was not refused: %v", err)
	}
	if _, _, err := ui.ProjectProps(design.Default(), source, ui.PropsProposal{
		BaseSHA256: base.SHA256, Path: []string{form.ID}, Props: json.RawMessage(`{"id":"another-form"}`)}); err == nil {
		t.Fatal("DOM identity became an editable presentation property")
	}
}

func TestExampleRequiresAUsableNamespace(t *testing.T) {
	t.Parallel()
	for _, namespace := range []string{"", " ", "two forms", "0form", "a.b", "#form", "a/b", "é"} {
		if example, err := forms.Example("source/id", forms.Model{}, forms.Options{Namespace: namespace}); !errors.Is(err, forms.ErrNamespace) || example.Node != nil {
			t.Fatalf("namespace %q accepted or returned a partial example: %v", namespace, err)
		}
	}
}
