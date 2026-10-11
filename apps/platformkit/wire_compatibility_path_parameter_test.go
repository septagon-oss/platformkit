package main

// A path parameter the address cannot name is not a promise.
//
// scim-schema-get was first published at /scim/v2/Schemas/{id} with a required path
// parameter called `name`, which no template variable fills. Every request to the
// advertised address — including the one a client generated from that very document
// sends — was refused with `path.name: required path parameter is missing` before
// the service was asked. The route now names its parameter by the template variable
// the address already carried.
//
// What is pinned here is that fix and no more: the committed contract and the
// address it publishes agree on one name. TestTheOpenAPIDocumentIsTheComposition
// Served is what says the committed contract is the document the composition serves,
// and kit/wire owns the comparison rules, so nothing here restates them.

import (
	"strings"
	"testing"
)

// wireSchemaOperation is the operation whose address this delivery fixed. The case
// reads the committed contract rather than a fixture, so an operation missing from
// the document makes it refuse to run instead of pass.
const wireSchemaOperation = "scim-schema-get"

// wireGetRoute finds one operation's GET entry, by its operationId, and answers the
// address it is published at.
func wireGetRoute(t *testing.T, doc map[string]any, operation string) (path string, route map[string]any) {
	t.Helper()
	for candidate, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		get, _ := item["get"].(map[string]any)
		if id, _ := get["operationId"].(string); id == operation {
			return candidate, get
		}
	}
	t.Fatalf("%s is not published by this document", operation)
	return "", nil
}

// TestAPublishedPathParameterIsOneTheAddressCanFill asks the committed contract
// whether the operation's only `in: path` parameter is the variable its own address
// carries. A document that fails this describes a door no caller can open.
func TestAPublishedPathParameterIsOneTheAddressCanFill(t *testing.T) {
	t.Parallel()
	path, route := wireGetRoute(t, wireDocument(t, mustReadOpenAPIGolden(t)), wireSchemaOperation)
	parameters, _ := route["parameters"].([]any)
	if len(parameters) != 1 {
		t.Fatalf("%s is published at %s with %d parameters; the door takes one", wireSchemaOperation, path, len(parameters))
	}
	parameter, _ := parameters[0].(map[string]any)
	if at, _ := parameter["in"].(string); at != "path" {
		t.Fatalf("%s is published at %s with its only parameter declared %q, not \"path\"", wireSchemaOperation, path, at)
	}
	if required, _ := parameter["required"].(bool); !required {
		t.Errorf("%s is published at %s with its path parameter not required", wireSchemaOperation, path)
	}
	name, _ := parameter["name"].(string)
	if !strings.Contains(path, "{"+name+"}") {
		t.Errorf("%s is published at %s, which cannot fill the required parameter %q it documents; a client written against this document sends nothing where the door demands it",
			wireSchemaOperation, path, name)
	}
}
