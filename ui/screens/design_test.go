package screens_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func ExampleFormExample() {
	resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/api/v1/notes",
		Schema: crud.Schema{Fields: []crud.Field{{Name: "description", Type: crud.TypeText, Widget: "textarea"}}}}
	form := screens.FormExample("notes/new", resource, screens.Options{Root: "/admin"}, "/admin/notes", "New note",
		map[string]any{"description": "A synthetic design example."}, nil, "", true)
	snapshot, err := export.Export(design.Default(), []examples.Example{form})
	if err != nil {
		panic(err)
	}
	fmt.Println(snapshot.Examples[0].ID)
	fmt.Println(snapshot.Examples[0].Children[0].Description.ID)
	fmt.Println(snapshot.Examples[0].Children[0].Description.ComponentID)
	// Output:
	// notes/new
	// field/description
	// pk-ui.component.textarea
}

func TestGeneratedFormInheritsObservedCoreContracts(t *testing.T) {
	t.Parallel()
	r := resource()
	r.Schema.Fields = append(slices.Clone(r.Schema.Fields),
		crud.Field{Name: "actions", Type: crud.TypeString},
		crud.Field{Name: "error", Type: crud.TypeString},
		crud.Field{Name: "ratio", Type: crud.TypeFloat},
		crud.Field{Name: "when", Type: crud.TypeTime},
		crud.Field{Name: "owner", Type: crud.TypeUUID, Widget: "entity-picker"})
	for _, state := range []struct {
		id     string
		create bool
		detail string
	}{{"new", true, ""}, {"edit", false, ""}, {"refused", false, "Fix the title."}} {
		t.Run(state.id, func(t *testing.T) {
			input := r
			input.Schema.Fields = slices.Clone(r.Schema.Fields)
			row := map[string]any{"title": "Field notes", "body": "\nA description\n", "status": "done", "pinned": true}
			errs := map[string]string{"title": state.detail}
			form := screens.FormExample("notes/"+state.id, input, opts, "/admin/note/notes", "A note", row, errs, state.detail, state.create)
			description, err := form.Describe()
			if err != nil {
				t.Fatal(err)
			}
			view := screens.Form(input, opts, "/admin/note/notes", "A note", row, errs, state.detail, state.create)
			if got := render(t, []g.Node{view.Body[len(view.Body)-1]}); got != description.HTML {
				t.Fatal("the runtime form and its design invocation render different HTML")
			}
			want := []string{"field/title", "field/body", "field/rank", "field/pinned", "field/tags",
				"field/actions", "field/error", "field/ratio", "field/when", "field/owner", "actions"}
			if !state.create {
				want = slices.Insert(want, 2, "field/status")
			}
			if state.detail != "" {
				want = append([]string{"error"}, want...)
			}
			var got []string
			for _, child := range description.Children {
				got = append(got, child.Description.ID)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("children = %q, want %q", got, want)
			}
			var observed func(examples.ExampleDescription)
			observed = func(parent examples.ExampleDescription) {
				kind := "input"
				if specialized := (map[string]string{form.ID: "form", "field/body": "textarea", "field/status": "select",
					"field/pinned": "checkbox", "error": "alert", "actions": "formactions", "cancel": "button", "save": "button"})[parent.ID]; specialized != "" {
					kind = specialized
				}
				if parent.ComponentID != "pk-ui.component."+kind {
					t.Fatalf("%s uses %s instead of Core's %s", parent.ID, parent.ComponentID, kind)
				}
				if !parent.PropsEditable || len(parent.OpaqueSlots) != 0 {
					t.Fatalf("%s lost typed props or direct slot ownership", parent.ID)
				}
				for _, child := range parent.Children {
					span := child.Span
					if child.Slot != "children" || span == nil || parent.HTML[span.Start:span.End] != child.Description.HTML {
						t.Fatalf("%s has no exact observed owning slot", child.Description.ID)
					}
					observed(child.Description)
				}
			}
			observed(description)
			// Export's shared interface check rejects another Props or slot shape
			// under a Core identity, including private Button and Alert shortcuts.
			if _, err := export.Export(design.Default(), append(examples.Gallery(), form)); err != nil {
				t.Fatal(err)
			}
			// The form's DOM identity is the address it posts to, so the design
			// invocation and the served page agree on it by construction; the check
			// above compares their HTML byte for byte. What these markers hold is the
			// rest: the encoding, the swap targets and the label-to-control pairing
			// a native renderer and a screen reader both depend on.
			const fieldID = "admin-note-notes-field-"
			titleID, bodyID := fieldID+"7469746c65", fieldID+"626f6479"
			for _, marker := range []string{`id="admin-note-notes-form"`, `method="post"`, `hx-post="/admin/note/notes"`,
				`hx-target="#admin-note-notes-form"`, `hx-select="#admin-note-notes-form"`, `aria-label="A note"`,
				`for="` + titleID + `"`, `id="` + titleID + `"`,
				`for="` + bodyID + `"`, `id="` + bodyID + `"`, `rows="5"`, `href="/admin/note/notes"`, `type="submit"`} {
				if !strings.Contains(description.HTML, marker) {
					t.Fatalf("form lost runtime or native control attribute %s", marker)
				}
			}
			if state.detail != "" && !strings.Contains(description.HTML, `aria-describedby="`+titleID+`-error `+titleID+`-help"`) {
				t.Fatal("the invalid field lost its error and help association")
			}
			slices.Reverse(input.Schema.Fields)
			reordered, err := screens.FormExample(form.ID, input, opts, "/admin/note/notes", "A note", row, errs, state.detail, state.create).Describe()
			if err != nil {
				t.Fatal(err)
			}
			for _, child := range description.Children {
				index := slices.IndexFunc(reordered.Children, func(other examples.ChildOccurrence) bool { return other.Description.ID == child.Description.ID })
				if index < 0 || !reflect.DeepEqual(child.Description, reordered.Children[index].Description) {
					t.Fatalf("schema order changed the contract or inputs of %s", child.Description.ID)
				}
			}
			// Captures own their inputs, not a later mutation of the resource,
			// saved row or refusal map used to build them.
			row["title"], errs["title"] = "Mutated", "Mutated"
			after, err := form.Describe()
			if err != nil || !reflect.DeepEqual(after, description) {
				t.Fatalf("caller mutation changed a captured form: %v", err)
			}
		})
	}
}

func TestGeneratedFormComposesSourceEditsAndReplacements(t *testing.T) {
	t.Parallel()
	form := screens.FormExample("notes/edit", resource(), opts, "/admin/note/notes/1", "Edit note",
		map[string]any{"title": "Original", "body": "Before", "status": "done"}, nil, "", false)
	source := []examples.Example{form}
	base, err := export.Export(design.Default(), source)
	if err != nil {
		t.Fatal(err)
	}
	path := []string{form.ID, "field/body"}
	edited, candidate, err := export.ProjectProps(design.Default(), source, export.PropsProposal{
		BaseSHA256: base.SHA256, Path: path, Props: json.RawMessage(`{"value":"\nAfter\n"}`)})
	if err != nil {
		t.Fatal(err)
	}
	before := base.Examples[0]
	after := candidate.Examples[0]
	if before.HTML == after.HTML || base.SHA256 == candidate.SHA256 {
		t.Fatal("the nested edit did not change the source projection")
	}
	for index, child := range before.Children {
		if child.Description.ID != "field/body" && !reflect.DeepEqual(child.Description, after.Children[index].Description) {
			t.Fatalf("the description edit changed sibling %s", child.Description.ID)
		}
	}
	buttonPath := []string{form.ID, "actions", "save"}
	buttonEdit, _, err := export.ProjectProps(design.Default(), source, export.PropsProposal{
		BaseSHA256: base.SHA256, Path: buttonPath, Props: json.RawMessage(`{"label":"Save changes"}`)})
	if err != nil {
		t.Fatal(err)
	}
	button, err := buttonEdit[0].At(buttonPath)
	if err != nil {
		t.Fatal(err)
	}
	if html := render(t, []g.Node{button.Node}); !strings.Contains(html, "Save changes") || !strings.Contains(html, `type="submit"`) {
		t.Fatal("nested action editing lost the Core button or its submit behavior")
	}
	if _, _, err := export.ProjectProps(design.Default(), edited, export.PropsProposal{
		BaseSHA256: base.SHA256, Path: path, Props: json.RawMessage(`{"value":"Stale"}`)}); !errors.Is(err, export.ErrStaleExport) {
		t.Fatalf("a stale source edit was not refused: %v", err)
	}
	if _, err := form.WithReplacementAt(path, examples.ExampleOf(
		examples.ExampleInfo{ID: "wrong", ComponentID: "pk-ui.component.input"}, components.InputProps{}, components.Input)); err == nil {
		t.Fatal("a Textarea accepted an Input's different interface")
	}
	replacement := examples.ExampleOf(examples.ExampleInfo{ID: "replacement", ComponentID: "pk-ui.component.textarea"},
		components.TextareaProps{Name: "body", Label: "Description", Value: "Replacement", Rows: 5}, components.Textarea)
	source = append(source, replacement)
	base, err = export.Export(design.Default(), source)
	if err != nil {
		t.Fatal(err)
	}
	replaced, _, err := export.ProjectReplacement(design.Default(), source, export.ReplacementProposal{
		BaseSHA256: base.SHA256, Path: path, ReplacementPath: []string{replacement.ID}})
	if err != nil {
		t.Fatal(err)
	}
	field, err := replaced[0].At(path)
	if err != nil || field.ID != "field/body" {
		t.Fatalf("replacement lost the destination's identity: %v", err)
	}
	description, err := field.Describe()
	if err != nil || !strings.Contains(description.HTML, "Replacement") {
		t.Fatalf("replacement did not inherit the supplied constructor: %v", err)
	}
	original, err := form.Describe()
	if err != nil || !reflect.DeepEqual(original, before) {
		t.Fatalf("editing or replacement mutated the original form: %v", err)
	}
}
