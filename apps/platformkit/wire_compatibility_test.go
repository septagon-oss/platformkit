package main

import (
	"encoding/json"
	"strings"
	"testing"

	wiregate "github.com/septagon-oss/platformkit/kit/wire"
)

// wireAuthorizationAllowances records this composition's reviewed access widening.
func wireAuthorizationAllowances() []wiregate.AuthorizationAllowance {
	return []wiregate.AuthorizationAllowance{{
		From: "kind=signed_in", To: "kind=any_credential", ReviewedOn: "2026-10-06",
		Reason: "Preserve the existing reference gate: any credential also admits session holders",
	}}
}

// breakingWireChanges preserves the fixture assertions; kit/wire owns the rules.
func breakingWireChanges(t *testing.T, golden, served []byte) []string {
	t.Helper()
	var problems []string
	for _, problem := range wiregate.CompareWithAllowances(golden, served, wireAuthorizationAllowances()) {
		problems = append(problems, problem.String())
	}
	return problems
}

func wireDocument(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	if doc == nil {
		t.Fatal("the document is empty")
	}
	return doc
}

func wireResolve(doc map[string]any, ref string) map[string]any {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	components, _ := doc["components"].(map[string]any)
	shapes, _ := components["schemas"].(map[string]any)
	target, _ := shapes[strings.TrimPrefix(ref, prefix)].(map[string]any)
	return target
}

func jsonResponseSchema(route map[string]any) map[string]any {
	for _, status := range []string{"200", "201"} {
		raw, ok := wireMap(route["responses"])[status]
		if !ok {
			continue
		}
		response, _ := raw.(map[string]any)
		for media, rawMedia := range wireMap(response["content"]) {
			if media != "application/json" {
				continue
			}
			mediaShape, _ := rawMedia.(map[string]any)
			return wireSchema(mediaShape)
		}
	}
	return nil
}

func wireSchema(node map[string]any) map[string]any {
	if node == nil {
		return nil
	}
	if schema, ok := node["schema"].(map[string]any); ok {
		return schema
	}
	if _, ok := node["type"]; ok {
		return node
	}
	return nil
}

func wireRoute(doc map[string]any, operationID string) (map[string]any, bool) {
	for _, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		for _, verb := range [...]string{"get", "put", "post", "delete", "patch", "head", "options", "trace"} {
			route, _ := item[verb].(map[string]any)
			if route == nil {
				continue
			}
			if id, _ := route["operationId"].(string); id == operationID {
				return route, true
			}
		}
	}
	return nil, false
}

func wireProperty(doc map[string]any, schema map[string]any, name string) map[string]any {
	property := wireResolveRef(doc, wireMap(wireMap(schema["properties"])[name]))
	if property == nil {
		return nil
	}
	return property
}

func wireItemsOf(doc map[string]any, schema map[string]any, name string) map[string]any {
	property := wireProperty(doc, schema, name)
	if property == nil {
		return nil
	}
	items, _ := property["items"].(map[string]any)
	return wireResolveRef(doc, items)
}

func wireResolveRef(doc map[string]any, node map[string]any) map[string]any {
	if ref, _ := node["$ref"].(string); ref != "" {
		return wireResolve(doc, ref)
	}
	return node
}

func wireMap(node any) map[string]any {
	out, _ := node.(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func wireStringList(node any) []string {
	list, _ := node.([]any)
	var out []string
	for _, item := range list {
		if value, _ := item.(string); value != "" {
			out = append(out, value)
		}
	}
	return out
}
