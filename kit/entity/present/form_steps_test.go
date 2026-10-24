package present_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

func TestFormStepsResolveEditableSectionsWithoutChangingRecords(t *testing.T) {
	fields := []entity.Field{
		{Name: "title", Type: entity.TypeString, Required: true},
		{Name: "body", Type: entity.TypeText, Presentation: entity.FieldHints{Section: "details"}},
		{Name: "rank", Type: entity.TypeInt, Presentation: entity.FieldHints{Section: "details"}},
		{Name: "pinned", Type: entity.TypeBool, Presentation: entity.FieldHints{Section: "options"}},
		{Name: "tags", Type: entity.TypeList, Elem: entity.TypeString, Presentation: entity.FieldHints{Section: "options"}},
		{Name: "status", Type: entity.TypeString},
	}
	in := present.Input{Schema: entity.Schema{Fields: fields}, Immutable: []string{"status"}, Hints: entity.EntryHints{
		Sections: []entity.EntitySection{{Key: "details", Label: "Description"}, {Key: "options", Label: "Options"}},
	}}
	before := present.Resolve(in)
	in.Hints.Forms = &entity.FormsHints{Create: &entity.FormHints{Layout: "steps", Steps: []entity.FormStepHints{
		{PrimaryLabel: "Next · {answer}", AnswerField: "title", EmptyLabel: "Enter a title"},
		{Section: "details", PrimaryLabel: "Next"}, {Section: "options", PrimaryLabel: "Save note"},
	}}}
	after := present.Resolve(in)
	if after.Layout.Forms == nil || after.Layout.Forms.Update != nil || after.Layout.Forms.Input != nil {
		t.Fatalf("per-form opt-in: %+v", after.Layout.Forms)
	}
	steps := after.Layout.Forms.Create.Steps
	want := [][]string{{"title"}, {"body", "rank"}, {"pinned", "tags"}}
	for i, key := range []string{"unsectioned", "section:details", "section:options"} {
		if steps[i].Key != key || !reflect.DeepEqual(steps[i].Fields, want[i]) {
			t.Fatalf("step %d: %+v", i, steps[i])
		}
	}
	after.Layout.Forms = nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("form opt-in changed record or flat field policy")
	}
}

func TestStepDeclarationsRefuseUnusedAndAmbiguousCopy(t *testing.T) {
	field := entity.Field{Name: "title", Type: entity.TypeString}
	for _, hint := range []entity.FormHints{
		{Layout: "wizard"}, {Steps: []entity.FormStepHints{{PrimaryLabel: "Save"}}},
		{Layout: "steps"}, {Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "{other}"}}},
		{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "{answer}", AnswerField: "missing", EmptyLabel: "Enter"}}},
		{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "Next", EmptyLabel: "unused"}}},
		{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "Save"}, {PrimaryLabel: "Again"}}},
		{Layout: "steps", Steps: []entity.FormStepHints{{Section: "empty", PrimaryLabel: "Save"}}},
	} {
		if _, err := present.ResolveForm(&hint, nil, []entity.Field{field}, []string{"title"}, "Details"); err == nil {
			t.Fatalf("accepted %+v", hint)
		}
	}
}
