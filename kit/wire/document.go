package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// wireDocument decodes one JSON document, keeping every number as the literal the
// document wrote (json.Number) rather than as a float64: a bound the gate compares is a
// contract's exact figure, and 9007199254740993 and 9007199254740992 are the same
// float64. A decoder is more accepting than json.Unmarshal at the edges — it takes a
// second value after the first — so the bytes after the document are read and refused.
func wireDocument(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("trailing content after the document")
		}
		return nil, fmt.Errorf("trailing content after the document: %w", err)
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
//
// schemaNode says whether this node's `required` names properties. An OpenAPI parameter
// object carries a `required` of its own — whether the caller must send the parameter at all
// — and the schemas it holds name properties instead, so one keyword means two things at that
// one place and each is read as what it says there.
func validateSchema(doc map[string]any, raw any, seen map[string]bool, schemaNode bool) error {
	node, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("schema must be an object")
	}
	if raw, present := node["type"]; present && raw != nil {
		if err := validateTypeNames(raw); err != nil {
			return err
		}
	}
	if schemaNode {
		if raw, present := node["required"]; present && !wireNames(raw) {
			return fmt.Errorf("required must be a list of property names")
		}
	}
	if rawRef, present := node["$ref"]; present {
		ref, _ := rawRef.(string)
		target := wireResolve(doc, ref)
		if target == nil {
			return fmt.Errorf("unresolved local reference %q", ref)
		}
		if !seen[ref] {
			seen[ref] = true
			if err := validateSchema(doc, target, seen, true); err != nil {
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
			if err := validateSchema(doc, props[name], seen, true); err != nil {
				return err
			}
		}
	}
	if raw, present := node["items"]; present {
		if err := validateSchema(doc, raw, seen, true); err != nil {
			return err
		}
	}
	if raw, present := node["additionalProperties"]; present {
		if err := validateOpenProperties(doc, raw, seen); err != nil {
			return err
		}
	}
	if raw, present := node["anyOf"]; present {
		branches, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("anyOf must be an array")
		}
		for _, branch := range branches {
			if err := validateSchema(doc, branch, seen, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateOpenProperties checks the one keyword that may say either what may follow or that
// nothing may: a boolean closes or opens the slot, a schema says what may, and anything else
// is not a declaration the comparison can read as either.
func validateOpenProperties(doc map[string]any, raw any, seen map[string]bool) error {
	if _, boolean := raw.(bool); boolean {
		return nil
	}
	if _, object := raw.(map[string]any); !object {
		return fmt.Errorf("additionalProperties must be a boolean or a schema")
	}
	return validateSchema(doc, raw, seen, true)
}

// validateTypeNames refuses a `type` that names nothing: an empty set, an empty name, or a
// value that is neither a name nor a set of names. A type naming no type is not an open type,
// and a comparison that read it as one would accept a document no instance can satisfy.
func validateTypeNames(raw any) error {
	switch typed := raw.(type) {
	case string:
		if typed == "" {
			return fmt.Errorf("type must name a type")
		}
	case []any:
		if len(typed) == 0 {
			return fmt.Errorf("type must name at least one type")
		}
		for _, item := range typed {
			if name, _ := item.(string); name == "" {
				return fmt.Errorf("type must name only types")
			}
		}
	default:
		return fmt.Errorf("type must be a name or a set of names, not %s", wireJSONToken(raw))
	}
	return nil
}

// wireNames is a schema-level `required` as the list of property names it states: a list in
// which every element is a name. A list holding something else states a requirement no reader
// can resolve, which is why it refuses rather than comparing as the shorter list it would
// silently become.
func wireNames(node any) bool {
	list, ok := node.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		if name, _ := item.(string); name == "" {
			return false
		}
	}
	return true
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
					if err := validateParameter(doc, param); err != nil {
						return err
					}
					if schema, present := param["schema"]; present {
						if err := validateSchema(doc, schema, map[string]bool{}, true); err != nil {
							return err
						}
					}
				}
			}
			if raw, present := route["requestBody"]; present {
				if err := validateRequestBody(doc, raw); err != nil {
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

// validateRequestBody reads the body object before its content: whether the operation demands
// a body at all is a declaration about the request, and a `required` that is not a boolean
// states one this gate cannot read.
func validateRequestBody(doc map[string]any, raw any) error {
	if node, ok := raw.(map[string]any); ok {
		if err := validateDemand(node, "request body required"); err != nil {
			return err
		}
	}
	return validateContent(doc, raw)
}

// validateParameter is one parameter object: the whole schema it may hold, plus its own
// `required`, which says whether the caller must send the parameter.
func validateParameter(doc map[string]any, param map[string]any) error {
	if err := validateDemand(param, "parameter required"); err != nil {
		return err
	}
	return validateSchema(doc, param, map[string]bool{}, false)
}

// validateDemand refuses a demand written in another kind, naming it where it stands.
func validateDemand(node map[string]any, where string) error {
	if raw, present := node["required"]; present && raw != nil {
		if _, boolean := raw.(bool); !boolean {
			return fmt.Errorf("%s must be a boolean", where)
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
				if err := validateSchema(doc, schema, map[string]bool{}, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
