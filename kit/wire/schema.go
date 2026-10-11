package wire

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

func enumMove(key, before, after string) string {
	oldSet, ok := enumSet(before)
	if !ok {
		return ""
	}
	newSet, _ := enumSet(after)
	if strings.Contains(key, " request ") {
		for _, value := range oldSet {
			if !slices.Contains(newSet, value) {
				return fmt.Sprintf("B5 (breaking): %s no longer accepts %q, which an older client sends", key, value)
			}
		}
	}
	if strings.Contains(key, " response ") {
		for _, value := range newSet {
			if !slices.Contains(oldSet, value) {
				return fmt.Sprintf("B5 (breaking): %s may now answer %q, which an older reader has no branch for", key, value)
			}
		}
	}
	return ""
}

func enumSet(signature string) ([]string, bool) {
	_, rest, found := strings.Cut(signature, "enum=")
	if !found {
		return nil, false
	}
	return strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == '|' }), true
}

func wireAuth(route map[string]any) string {
	declared, _ := route["x-platformkit-auth"].(map[string]any)
	var parts []string
	for _, key := range []string{"kind", "permission", "operator_permission"} {
		if value, _ := declared[key].(string); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, " ")
}

func walkWire(out map[string]string, id, at string, node map[string]any, doc map[string]any, depth int) {
	if node == nil || depth > 12 {
		return
	}
	key := id + " " + at
	out[key] = wireSignature(node)
	for name, child := range wireMap(node["properties"]) {
		member, _ := child.(map[string]any)
		walkWire(out, id, at+" ."+name, member, doc, depth+1)
	}
	for _, name := range wireStringList(node["required"]) {
		out[key+" requires "+name] = "required"
	}
	if items, ok := node["items"].(map[string]any); ok {
		walkWire(out, id, at+" []", items, doc, depth+1)
	}
	if extra, ok := node["additionalProperties"].(map[string]any); ok {
		walkWire(out, id, at+" {}", extra, doc, depth+1)
	}
	for branch, raw := range wireList(node["anyOf"]) {
		arm, _ := raw.(map[string]any)
		walkWire(out, id, at+" |"+branch, arm, doc, depth+1)
	}
	if ref, _ := node["$ref"].(string); ref != "" {
		if target := wireResolve(doc, ref); target != nil && !strings.Contains(at, "»"+ref+" ") {
			walkWire(out, id, at+" »"+ref, target, doc, depth+1)
		}
	}
}

func wireSignature(node map[string]any) string {
	if node == nil {
		return "none"
	}
	var parts []string
	for _, key := range []string{"type", "format", "$ref"} {
		if value, _ := node[key].(string); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if members := wireStringList(node["enum"]); len(members) > 0 {
		slices.Sort(members)
		parts = append(parts, "enum="+strings.Join(members, "|"))
	}
	for _, key := range []string{"minLength", "maxLength", "pattern", "minimum", "maximum"} {
		if value, ok := node[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
	}
	if required, _ := node["nullable"].(bool); required {
		parts = append(parts, "nullable")
	}
	return strings.Join(parts, " ")
}

func wireOperationOf(key string) string {
	id, _, _ := strings.Cut(key, " ")
	return id
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

func wireMap(node any) map[string]any {
	out, _ := node.(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func wireList(node any) map[string]any {
	list, ok := node.([]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(list))
	for i, item := range list {
		if named, _ := item.(map[string]any); named != nil {
			if name, _ := named["name"].(string); name != "" {
				out[name] = item
				continue
			}
		}
		out[fmt.Sprintf("#%d", i)] = item
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

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
