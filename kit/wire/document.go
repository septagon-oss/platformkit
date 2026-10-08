package wire

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

func wireDocument(body []byte) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("empty document")
	}
	open, hasOpen := doc["openapi"]
	async, hasAsync := doc["asyncapi"]
	if hasOpen == hasAsync {
		return nil, fmt.Errorf("expected exactly one OpenAPI or AsyncAPI marker")
	}
	marker, _ := open.(string)
	collections := []string{"paths"}
	if hasAsync {
		marker, _ = async.(string)
		collections = []string{"channels", "operations"}
	}
	if !strings.HasPrefix(marker, "3.") || len(marker) <= 2 {
		return nil, fmt.Errorf("unsupported format version %q", marker)
	}
	for _, name := range collections {
		if _, ok := doc[name].(map[string]any); !ok {
			return nil, fmt.Errorf("%s must be an object", name)
		}
	}
	var err error
	if hasAsync {
		err = validateAsyncAPI(doc)
	} else {
		err = validateOpenAPI(doc)
	}
	if err != nil {
		return nil, err
	}
	seenIDs, seenRoutes := map[string]bool{}, map[string]bool{}
	for _, op := range wireOperations(doc) {
		if op.operationID == "" || strings.ContainsFunc(op.operationID, unicode.IsSpace) || seenIDs[op.operationID] {
			return nil, fmt.Errorf("missing, spaced or duplicate operation identity %q", op.operationID)
		}
		if seenRoutes[op.key] {
			return nil, fmt.Errorf("duplicate operation address %q", op.key)
		}
		seenIDs[op.operationID], seenRoutes[op.key] = true, true
	}
	return doc, nil
}

// wireResolve follows a local JSON pointer to an object. Schema cycles are valid:
// this resolves the pointer only, leaving bounded traversal to walkWire.
func wireResolve(doc map[string]any, ref string) map[string]any {
	if ref == "#" {
		return doc
	}
	rest, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil
	}
	var node any = doc
	for part := range strings.SplitSeq(rest, "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				i++
				if i == len(part) || (part[i] != '0' && part[i] != '1') {
					return nil
				}
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch current := node.(type) {
		case map[string]any:
			node = current[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != part {
				return nil
			}
			node = current[index]
		default:
			return nil
		}
	}
	out, _ := node.(map[string]any)
	return out
}

// validateSchema checks only the nodes the walker understands, not arbitrary
// examples/extensions or every JSON Schema keyword. References are checked once
// per target; a recursive schema does not cause recursive validation forever.
func validateSchema(doc map[string]any, raw any, seen map[string]bool) error {
	node, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("schema must be an object")
	}
	if rawRef, present := node["$ref"]; present {
		ref, _ := rawRef.(string)
		target := wireResolve(doc, ref)
		if target == nil {
			return fmt.Errorf("unresolved local reference %q", ref)
		}
		if !seen[ref] {
			seen[ref] = true
			if err := validateSchema(doc, target, seen); err != nil {
				return err
			}
		}
	}
	if raw, present := node["properties"]; present {
		props, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("properties must be an object")
		}
		for _, name := range slices.Sorted(maps.Keys(props)) {
			if err := validateSchema(doc, props[name], seen); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"items", "additionalProperties"} {
		if raw, present := node[name]; present {
			if _, boolean := raw.(bool); boolean && name == "additionalProperties" {
				continue
			}
			if err := validateSchema(doc, raw, seen); err != nil {
				return err
			}
		}
	}
	if raw, present := node["anyOf"]; present {
		branches, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("anyOf must be an array")
		}
		for _, branch := range branches {
			if err := validateSchema(doc, branch, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOpenAPI(doc map[string]any) error {
	paths := wireMap(doc["paths"])
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		item, ok := paths[path].(map[string]any)
		if !ok {
			return fmt.Errorf("path %q must be an object", path)
		}
		for _, verb := range [...]string{"get", "put", "post", "delete", "patch", "head", "options", "trace"} {
			raw, present := item[verb]
			if !present {
				continue
			}
			route, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("operation %s %s must be an object", verb, path)
			}
			if raw, present := route["parameters"]; present {
				params, ok := raw.([]any)
				if !ok {
					return fmt.Errorf("parameters must be an array")
				}
				for _, raw := range params {
					param, ok := raw.(map[string]any)
					if !ok {
						return fmt.Errorf("parameter must be an object")
					}
					if err := validateSchema(doc, param, map[string]bool{}); err != nil {
						return err
					}
					if schema, present := param["schema"]; present {
						if err := validateSchema(doc, schema, map[string]bool{}); err != nil {
							return err
						}
					}
				}
			}
			if raw, present := route["requestBody"]; present {
				if err := validateContent(doc, raw); err != nil {
					return err
				}
			}
			if raw, present := route["responses"]; present {
				responses, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("responses must be an object")
				}
				for _, status := range slices.Sorted(maps.Keys(responses)) {
					if err := validateContent(doc, responses[status]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validateContent(doc map[string]any, raw any) error {
	node, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("body or response must be an object")
	}
	if raw, present := node["content"]; present {
		content, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("content must be an object")
		}
		for _, media := range slices.Sorted(maps.Keys(content)) {
			shape, ok := content[media].(map[string]any)
			if !ok {
				return fmt.Errorf("media type must be an object")
			}
			if schema, present := shape["schema"]; present {
				if err := validateSchema(doc, schema, map[string]bool{}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
