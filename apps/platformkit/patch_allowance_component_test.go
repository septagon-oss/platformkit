package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The allowance forgives one correction per operation: its own open body
// becoming its own Patch component. A body that points at another
// operation's component is still a wire break on every operation it names.
func TestPatchDocumentationAllowanceAdmitsOnlyTheOperationsOwnComponent(t *testing.T) {
	operations := []struct{ id, component string }{
		{"task-task-update", "TaskPatch"},
		{"user-user-update", "UserPatch"},
		{"content-content-update", "ContentPatch"},
		{"billing-plan-update", "PlanPatch"},
	}
	golden := mustReadOpenAPIGolden(t)
	encode := func(doc map[string]any) []byte {
		t.Helper()
		body, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	bodyMedia := func(t *testing.T, doc map[string]any, id string) map[string]any {
		t.Helper()
		route, ok := wireRoute(doc, id)
		if !ok {
			t.Fatalf("%s is missing from the golden", id)
		}
		return wireMap(wireMap(wireMap(route["requestBody"])["content"])["application/json"])
	}
	for i, op := range operations {
		other := operations[(i+1)%len(operations)].component
		t.Run(op.id, func(t *testing.T) {
			old := wireDocument(t, golden)
			bodyMedia(t, old, op.id)["schema"] = map[string]any{
				"type": "object", "additionalProperties": map[string]any{}, "description": "The writable fields to change",
			}
			before := encode(old)
			if problems := breakingWireChanges(t, before, golden); len(problems) != 0 {
				t.Fatalf("own component refused: %v", problems)
			}
			changed := wireDocument(t, golden)
			bodyMedia(t, changed, op.id)["schema"] = map[string]any{"$ref": "#/components/schemas/" + other}
			want := "B3 (breaking): " + op.id + " request body application/json changed from"
			problems := breakingWireChanges(t, before, encode(changed))
			for _, problem := range problems {
				if strings.HasPrefix(problem, want) {
					return
				}
			}
			t.Fatalf("%s pointed at %s passed the allowance: %v", op.id, other, problems)
		})
	}
}
