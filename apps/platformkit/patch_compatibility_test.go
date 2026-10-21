package main

import (
	"encoding/json"
	"testing"
)

func TestPatchDocumentationAllowanceCannotHideOtherWireBreaks(t *testing.T) {
	// Reconstruct only the historical open-body roots so this pin keeps running
	// after regeneration; all other schemas and operations come from the golden.
	typed := wireDocument(t, mustReadOpenAPIGolden(t))
	old := wireDocument(t, mustReadOpenAPIGolden(t))
	ids := []string{"task-task-update", "user-user-update", "content-content-update", "billing-plan-update"}
	for _, id := range ids {
		route, _ := wireRoute(old, id)
		media := wireMap(wireMap(wireMap(route["requestBody"])["content"])["application/json"])
		media["schema"] = map[string]any{"type": "object", "additionalProperties": map[string]any{}, "description": "The writable fields to change"}
	}
	encode := func(v any) []byte {
		t.Helper()
		out, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := encode(old)
	if problems := breakingWireChanges(t, before, encode(typed)); len(problems) != 0 {
		t.Fatalf("document-only correction refused: %v", problems)
	}
	for _, tt := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"response", func(doc map[string]any) {
			route, _ := wireRoute(doc, "task-task-update")
			response := wireResolveRef(doc, jsonResponseSchema(route))
			delete(wireMap(response["properties"]), "title")
		}},
		{"another operation", func(doc map[string]any) {
			route, _ := wireRoute(doc, "auth-login")
			route["operationId"] = "changed-login"
		}},
		{"required patch field", func(doc map[string]any) {
			route, _ := wireRoute(doc, "task-task-update")
			shape := wireResolveRef(doc, patchRequestSchema(route))
			shape["required"] = []any{"title"}
		}},
		{"authorization", func(doc map[string]any) {
			route, _ := wireRoute(doc, "task-task-update")
			route["x-platformkit-auth"] = map[string]any{"kind": "permission", "permission": "new:permission"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := wireDocument(t, encode(typed))
			tt.mutate(changed)
			if problems := breakingWireChanges(t, before, encode(changed)); len(problems) == 0 {
				t.Fatal("wire break passed")
			}
		})
	}
	t.Run("already typed patch", func(t *testing.T) {
		changed := wireDocument(t, encode(typed))
		route, _ := wireRoute(changed, "task-task-update")
		shape := wireResolveRef(changed, patchRequestSchema(route))
		wireMap(wireMap(shape["properties"])["title"])["type"] = "integer"
		if problems := breakingWireChanges(t, encode(typed), encode(changed)); len(problems) == 0 {
			t.Fatal("typed property change passed")
		}
	})
}
