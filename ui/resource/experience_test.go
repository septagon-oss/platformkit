package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestWorkspaceListUsesTheDeclaredReading(t *testing.T) {
	r := hintedResource(entity.EntryHints{
		Singular: "entry", Plural: "entries", Description: "Keep decisions together.",
		PrimaryField: "body", SummaryFields: []string{"rank"}, StatusField: "status",
		EmptyDescription: "Add the first decision for your team.",
	}, nil)
	// A visible text field intentionally disagrees with the old title heuristic.
	r.Schema.Fields = []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString}, {Name: "body", Type: entity.TypeString},
		{Name: "rank", Type: entity.TypeInt}, {Name: "status", Type: entity.TypeString},
	}
	row := map[string]any{"id": "1", "title": "Internal heading", "body": "Customer renewal", "rank": 2, "status": "open"}
	v := resource.List(r, opts, []map[string]any{row}, 1, 1, "", true)
	body := render(t, v.Body)
	for _, want := range []string{"entries", "Keep decisions together.", "New entry", "Customer renewal", "1 entry"} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %q", want)
		}
	}
	if strings.Contains(body, "Internal heading") {
		t.Error("summary list still exposes the unrelated field")
	}
	if got := resource.Detail(r, opts, row, false).Title; got != "Customer renewal" {
		t.Errorf("record title = %q", got)
	}
	empty := render(t, resource.List(r, opts, nil, 0, 1, "", false).Body)
	if !strings.Contains(empty, r.Present.EmptyDescription) {
		t.Error("empty list ignores the declared next step")
	}
}

func TestWorkspacePrimaryHintNeverRevealsAHiddenField(t *testing.T) {
	for _, visibility := range []string{"hidden", "detail"} {
		r := hintedResource(entity.EntryHints{PrimaryField: "title", SummaryFields: []string{"rank"}},
			fieldsWith(map[string]entity.FieldHints{"title": {Visibility: visibility}}))
		body := render(t, resource.List(r, opts, []map[string]any{{"id": "1", "title": "Private title", "status": "open"}}, 1, 1, "", false).Body)
		if strings.Contains(body, "Private title") {
			t.Errorf("%s field leaked through the primary hint", visibility)
		}
	}
}
