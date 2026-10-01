package main

// wireCompatibility is the gate over the wire contract: the OpenAPI document this
// repository checks in (testdata/openapi.json) compared, rule by rule, against the
// one the composition serves. It is scripts/check_public_api.py's neighbour and
// not a copy of it — that script diffs the Go exported API across published tags,
// needs the network and a tag, and says of itself that it "does not establish
// behavioral compatibility". A JSON tag renamed is invisible to it and is exactly
// what breaks a build already installed on a phone.
//
// The rules are numbered so that a failure names one:
//
//	B1  an operationId the golden publishes is gone from the document
//	B2  a (method, path) the golden publishes is gone, or answers with another operationId
//	B3  a schema member the golden reached is removed, renamed or retyped
//	B4  a request schema or parameter list gained something required an older client never sends
//	B5  an enum moved the way that strands a reader: a member left a request, or joined a response
//	B6  a route's authorization declaration changed, read from x-platformkit-auth
//
// Additive is allowed: a new address, a new operation, a new optional member, a
// new enum value a request may send, a new x-platformkit-* extension.
//
// Nothing overrides a refusal, including UPDATE_GOLDEN=1. The way a planned
// breaking change ships is the way /api/v1/admin moved to /api/v1/app: land the
// new address, register the old one as a migration row (httpx.API.Alias,
// kit/httpx/aliases.go, or module.Module.Moved), and delete that row one release
// later with the date in CHANGELOG.md.

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// wireVerbs are the operations OpenAPI names on a path item.
var wireVerbs = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// breakingWireChanges is every rule the pair violates, sorted, empty when what is
// served now is something a client written against the golden still survives.
func breakingWireChanges(t *testing.T, golden, served []byte) []string {
	t.Helper()
	oldDoc, newDoc := wireDocument(t, golden), wireDocument(t, served)
	var problems []string

	// B1, B2 and B6: the address, the operation that answers it, and who may call.
	known := map[string]bool{}
	for _, op := range wireOperations(newDoc) {
		known[op.operationID] = true
	}
	updated := wireOperations(newDoc)
	var missing []string
	for _, old := range wireOperations(oldDoc) {
		fresh, ok := updated.find(old.key)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("B2 (breaking): %s is no longer in the document", old.key))
		case fresh.operationID != old.operationID:
			problems = append(problems, fmt.Sprintf("B2 (breaking): %s is answered by %q, which used to be %q", old.key, fresh.operationID, old.operationID))
		}
		if ok && fresh.auth != old.auth {
			problems = append(problems, fmt.Sprintf("B6 (breaking): %s (%s) is authorized %s where it was %s", old.key, old.operationID, fresh.auth, old.auth))
		}
		if !known[old.operationID] {
			missing = append(missing, old.operationID)
		}
	}
	slices.Sort(missing)
	for _, id := range missing {
		problems = append(problems, fmt.Sprintf("B1 (breaking): operationId %q is no longer in the document", id))
	}

	// B3, B4 and B5, over every schema the golden reached.
	oldShapes, newShapes := wireShapes(oldDoc), wireShapes(newDoc)
	for _, key := range sortedKeys(oldShapes) {
		if !strings.Contains(key, " ") {
			continue
		}
		fresh, ok := newShapes[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("B3 (breaking): %s is gone", key))
			continue
		}
		if problem := enumMove(key, oldShapes[key], fresh); problem != "" {
			problems = append(problems, problem)
			continue
		}
		if fresh != oldShapes[key] {
			problems = append(problems, fmt.Sprintf("B3 (breaking): %s changed from %q to %q", key, oldShapes[key], fresh))
		}
	}
	// B4 is a new demand on an operation that already existed. A new operation may
	// require whatever it likes: an older client never called it, which is the whole
	// difference, and it is why a composition that grew a module grows a document
	// without breaking anybody.
	announced := map[string]bool{}
	for _, op := range wireOperations(oldDoc) {
		announced[op.operationID] = true
	}
	for _, key := range sortedKeys(newShapes) {
		if _, old := oldShapes[key]; old {
			continue
		}
		if !announced[wireOperationOf(key)] || !strings.Contains(key, " request ") || !strings.Contains(key, " requires ") {
			continue
		}
		problems = append(problems, fmt.Sprintf("B4 (breaking): %s is asked for and an older client never sends it", key))
	}

	slices.Sort(problems)
	return problems
}

// enumMove is B5, in the two directions that strand somebody: a value a request no
// longer accepts, which an older client still sends, and a value a response may now
// answer, which an older reader has no branch for. The other two directions are
// additive and pass. A signature carries its set after "enum=", which is where the
// two sets come from; a member leaving a response is B3's report, once.
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

// enumSet is the enumerated set carried by a signature, and whether it carries one.
func enumSet(signature string) ([]string, bool) {
	_, rest, found := strings.Cut(signature, "enum=")
	if !found {
		return nil, false
	}
	return strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == '|' }), true
}

// wireOperation is one route, as far as this gate reads it: its address, the name
// a client calls it by, and the authorization declaration the middleware enforces.
type wireOperation struct {
	key         string // "GET /api/v1/app/resources"
	operationID string
	auth        string
}

type wireRoutes []wireOperation

func (r wireRoutes) find(key string) (wireOperation, bool) {
	for _, op := range r {
		if op.key == key {
			return op, true
		}
	}
	return wireOperation{}, false
}

// wireOperations reads every route the document publishes. The order is the one a
// reader of a failure wants: by address.
func wireOperations(doc map[string]any) wireRoutes {
	var out wireRoutes
	for path, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		for _, verb := range wireVerbs {
			route, _ := item[verb].(map[string]any)
			if route == nil {
				continue
			}
			id, _ := route["operationId"].(string)
			out = append(out, wireOperation{
				key:         strings.ToUpper(verb) + " " + path,
				operationID: id,
				auth:        wireAuth(route),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// wireAuth is the declaration a route carries, in the one form worth comparing.
// x-platformkit-auth is not decoration: it is the value the middleware enforces
// (kit/httpx/auth.go writes it from the declaration Register was given), which is
// why a change of it is a change of the contract and not of the prose.
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

// wireShapes is every schema member the document reaches, keyed by operation,
// direction and the member's own address, valued by the signature a reader is
// written against. A key that disappears is a member no older build can find.
func wireShapes(doc map[string]any) map[string]string {
	out := map[string]string{}
	for _, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		for _, verb := range wireVerbs {
			route, _ := item[verb].(map[string]any)
			id, _ := route["operationId"].(string)
			if route == nil || id == "" {
				continue
			}
			// Parameters are keyed by name and by `in`, so a query parameter that
			// moves to the path reads as a removal and an addition.
			for _, parameter := range wireList(route["parameters"]) {
				named, _ := parameter.(map[string]any)
				name, _ := named["name"].(string)
				where, _ := named["in"].(string)
				at := "request param " + where + "/" + name
				out[id+" "+at] = wireSignature(named)
				if required, _ := named["required"].(bool); required {
					out[id+" "+at+" requires "+name] = "required"
				}
				walkWire(out, id, at, wireSchema(named), doc, 0)
			}
			request := "request body"
			if body, ok := route["requestBody"].(map[string]any); ok {
				out[id+" "+request] = "body"
				for media, rawMedia := range wireMap(body["content"]) {
					mediaShape, _ := rawMedia.(map[string]any)
					out[id+" "+request+" "+media] = wireSignature(wireSchema(mediaShape))
					walkWire(out, id, request+" "+media, wireSchema(mediaShape), doc, 0)
				}
			}
			for status, rawResponse := range wireMap(route["responses"]) {
				response, _ := rawResponse.(map[string]any)
				for name, header := range wireList(response["headers"]) {
					named, _ := header.(map[string]any)
					out[id+" response "+status+" header/"+name] = wireSignature(named)
				}
				for media, rawMedia := range wireMap(response["content"]) {
					at := "response " + status + " " + media
					mediaShape, _ := rawMedia.(map[string]any)
					out[id+" "+at] = wireSignature(wireSchema(mediaShape))
					walkWire(out, id, at, wireSchema(mediaShape), doc, 0)
				}
			}
		}
	}
	return out
}

// walkWire records the shape of every member below one schema. A $ref is followed
// once per address on the way down, so a recursive schema is walked to the depth
// the document actually states and never loops.
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

// wireSignature is the three things a reader is written against — type, format and
// the component it points at — plus the set it may take when the schema enumerates
// them, plus the bounds a client formats against.
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

// wireOperationOf is the operation a shape key belongs to: keys are built as
// "<operationId> <direction> <address>", and the id contains no space.
func wireOperationOf(key string) string {
	id, _, _ := strings.Cut(key, " ")
	return id
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

// wireResolve is the one $ref form a huma document uses: a component schema.
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

// jsonResponseSchema is the application/json schema of an operation's success body.
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

// wireSchema lifts a media-type object or a parameter object to its schema.
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

// wireRoute is one published operation, found by the name a client calls it by.
func wireRoute(doc map[string]any, operationID string) (map[string]any, bool) {
	for _, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		for _, verb := range wireVerbs {
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

// wireProperty is one named property of an object, followed through the $ref the
// document points it at. A schema that names its members by reference is the norm
// in a huma document, and a claim about the fields has to follow them.
func wireProperty(doc map[string]any, schema map[string]any, name string) map[string]any {
	property := wireResolveRef(doc, wireMap(wireMap(schema["properties"])[name]))
	if property == nil {
		return nil
	}
	return property
}

// wireItemsOf is the item schema of one array property, which is where a catalog's
// entries live.
func wireItemsOf(doc map[string]any, schema map[string]any, name string) map[string]any {
	property := wireProperty(doc, schema, name)
	if property == nil {
		return nil
	}
	items, _ := property["items"].(map[string]any)
	return wireResolveRef(doc, items)
}

// wireResolveRef lifts a schema to the component it points at, or itself.
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

// wireList keeps a list's entries addressable by name where they have one, so a
// parameter that is renamed reads as a removal plus an addition rather than as
// every later entry changing address at once.
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

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
