package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPatchDocumentationAllowanceStillRefusesChangesToOtherMembers(t *testing.T) {
	golden := mustReadOpenAPIGolden(t)
	old := wireDocument(t, golden)
	route, ok := wireRoute(old, "task-task-update")
	if !ok {
		t.Fatal("task PATCH operation is missing")
	}
	wireMap(wireMap(wireMap(route["requestBody"])["content"])["application/json"])["schema"] = map[string]any{
		"type": "object", "additionalProperties": map[string]any{}, "description": "The writable fields to change",
	}
	encode := func(doc map[string]any) []byte {
		t.Helper()
		body, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	before := encode(old)
	if problems := breakingWireChanges(t, before, golden); len(problems) != 0 {
		t.Fatalf("historical body correction refused: %v", problems)
	}
	for _, tc := range []struct {
		name, member string
		mutate       func(map[string]any)
	}{
		{"path parameter", "request param path/id", func(route map[string]any) {
			delete(route, "parameters")
		}},
		{"response", "response 200 application/json", func(route map[string]any) {
			delete(wireMap(route["responses"]), "200")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := wireDocument(t, golden)
			route, _ := wireRoute(changed, "task-task-update")
			tc.mutate(route)
			problems := breakingWireChanges(t, before, encode(changed))
			for _, problem := range problems {
				if strings.HasPrefix(problem, "B3 (breaking): task-task-update "+tc.member) {
					return
				}
			}
			t.Fatalf("missing refusal for %s alongside the allowed body correction: %v", tc.member, problems)
		})
	}
}
