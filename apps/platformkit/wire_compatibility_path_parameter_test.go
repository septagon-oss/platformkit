package main

// A path parameter the address cannot name is not a promise.
//
// wireCompatibility's B3 and B4 read the checked-in document as a list of what a
// caller may send. One entry in that list was never anything of the kind:
// scim-schema-get is published at /scim/v2/Schemas/{id} and documented a required
// path parameter called `name`, which no template variable fills, so every request
// to the advertised address — including the one a client generated from that very
// document sends — was refused with `path.name: required path parameter is missing`
// before the service was asked. T-0297 fixes that route, and the fix renames the
// parameter to the variable the address already used.
//
// So the gate needed one sentence of precision, and this file is its pin: the rule
// in mistypedPathParams allows exactly one shape of change, and every case below is
// a change that shape does not cover, asserted to stay breaking. Remove a condition
// from the rule and a case here goes red — any parameter removal allowed (case 2),
// a demand an older client cannot sent allowed (case 3), the mistyped name replaced
// by a second name the address still cannot fill (case 1), a non-path parameter
// treated as invisible (case 4). None of those four is a form this repository
// serves.

import (
	"strings"
	"testing"
)

// wireSchemaOperation is the operation whose address this delivery fixes. The cases
// read the checked-in contract rather than a fixture, so an operation missing from
// the document makes them refuse to run instead of pass.
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

// wireOnlyPathParameter answers the single `in: path` parameter of the fixed route,
// and fails when the address carries any other number: every case here is stated
// over a one-parameter address, which is the shape the rule covers.
func wireOnlyPathParameter(t *testing.T, doc map[string]any) (path string, parameter map[string]any) {
	t.Helper()
	path, route := wireGetRoute(t, doc, wireSchemaOperation)
	params := wireList(route["parameters"])
	if len(params) != 1 {
		t.Fatalf("%s is published at %s with %d parameters; this case is stated over one", wireSchemaOperation, path, len(params))
	}
	for _, only := range params {
		parameter, _ = only.(map[string]any)
	}
	return path, parameter
}

// TestARenameOfAPathParameterTheAddressNeverNamedIsNotABreak runs the pair this
// delivery actually lands: the document as it stood, naming a parameter the template
// does not hold, against the one the composition serves.
func TestARenameOfAPathParameterTheAddressNeverNamedIsNotABreak(t *testing.T) {
	t.Parallel()
	served := mustReadOpenAPIGolden(t)
	path, parameter := wireOnlyPathParameter(t, wireDocument(t, served))
	name, _ := parameter["name"].(string)
	if !strings.Contains(path, "{"+name+"}") {
		t.Fatalf("%s is published at %s, whose template names its parameter %q; the pair below is not the one this case is about", wireSchemaOperation, path, name)
	}
	golden := wireMutated(t, served, func(t *testing.T, doc map[string]any) {
		_, only := wireOnlyPathParameter(t, doc)
		only["name"] = "name"
		only["description"] = "The resource type or schema urn"
		only["schema"] = map[string]any{"description": "The resource type or schema urn", "type": "string"}
	})
	if problems := breakingWireChanges(t, golden, served); len(problems) > 0 {
		t.Errorf("the corrected parameter reads as a break, so the advertised address could never be fixed: %v", problems)
	}
}

// TestTheWireGateStillRefusesANameTheAddressCannotFillEither keeps the exception
// narrow at the second end: a name the template cannot fill may only be replaced by
// one it can. Here the served document renames the satisfiable parameter to a second
// unsatisfiable one, which is a change nobody may make quietly.
func TestTheWireGateStillRefusesANameTheAddressCannotFillEither(t *testing.T) {
	t.Parallel()
	golden := mustReadOpenAPIGolden(t)
	served := wireMutated(t, golden, func(t *testing.T, doc map[string]any) {
		_, only := wireOnlyPathParameter(t, doc)
		only["name"] = "not-in-the-template"
	})
	if problems := breakingWireChanges(t, golden, served); len(problems) == 0 {
		t.Error("renaming the satisfiable parameter to a name the address cannot fill passed the gate")
	}
}

// TestTheWireGateStillRefusesASecondPathParameterDemand asks whether an operation
// that already answered to one parameter can start demanding a second, which is the
// demand B4 exists to refuse.
func TestTheWireGateStillRefusesASecondPathParameterDemand(t *testing.T) {
	t.Parallel()
	golden := mustReadOpenAPIGolden(t)
	served := wireMutated(t, golden, func(t *testing.T, doc map[string]any) {
		path, route := wireGetRoute(t, doc, wireSchemaOperation)
		second := map[string]any{"name": "version", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
		var every []any
		for _, parameter := range wireList(route["parameters"]) {
			every = append(every, parameter)
		}
		route["parameters"] = append(every, second)
		if !strings.Contains(path, "{version}") {
			// The added demand is one the address cannot carry either, so the pair
			// tests B4's count and not the exception's benefit.
			t.Logf("the added demand is unsatisfiable at %s, which is the point", path)
		}
	})
	problems := breakingWireChanges(t, golden, served)
	if len(problems) == 0 {
		t.Fatal("an operation gained a required path parameter and nothing was refused")
	}
	var b4 bool
	for _, p := range problems {
		if strings.HasPrefix(p, "B4 ") {
			b4 = true
		}
	}
	if !b4 {
		t.Errorf("the second demand was refused by %v, none of which names B4", problems)
	}
}

// TestTheWireGateStillRefusesAQueryParameterThatGoesAway keeps the exception inside
// the one place it can be right: a query parameter is filled by the caller, so a
// document that stops asking for it changed what a caller may send.
func TestTheWireGateStillRefusesAQueryParameterThatGoesAway(t *testing.T) {
	t.Parallel()
	golden := mustReadOpenAPIGolden(t)
	doc := wireDocument(t, golden)
	operation, name := wireAnyQueryParameter(t, doc)
	served := wireMutated(t, golden, func(t *testing.T, doc map[string]any) {
		_, route := wireGetRoute(t, doc, operation)
		var keep []any
		var dropped int
		for at, parameter := range wireList(route["parameters"]) {
			named, _ := parameter.(map[string]any)
			if where, _ := named["in"].(string); where == "query" && at == name {
				dropped++
				continue
			}
			keep = append(keep, parameter)
		}
		if dropped != 1 {
			t.Fatalf("the %q query parameter of %s was found %d times in the served document", name, operation, dropped)
		}
		route["parameters"] = keep
	})
	if problems := breakingWireChanges(t, golden, served); len(problems) == 0 {
		t.Errorf("dropping the %q query parameter of %s is not refused; only a path parameter the address cannot name is ever invisible", name, operation)
	}
}

// wireAnyQueryParameter names one query parameter one GET operation of the document
// asks for, so the case above has something real to remove.
func wireAnyQueryParameter(t *testing.T, doc map[string]any) (operation, name string) {
	t.Helper()
	for _, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		// GET only, because wireGetRoute reaches an operation by its GET entry.
		route, _ := item["get"].(map[string]any)
		id, _ := route["operationId"].(string)
		if route == nil || id == "" {
			continue
		}
		for _, parameter := range wireList(route["parameters"]) {
			named, _ := parameter.(map[string]any)
			at, _ := named["name"].(string)
			if where, _ := named["in"].(string); where == "query" && at != "" {
				return id, at
			}
		}
	}
	t.Fatalf("%s publishes no query parameter to remove", openapiGolden)
	return "", ""
}
