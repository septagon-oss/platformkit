package wire

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

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

func openAPIOperations(doc map[string]any) wireRoutes {
	var out wireRoutes
	for path, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
		for _, verb := range [...]string{"get", "put", "post", "delete", "patch", "head", "options", "trace"} {
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
	slices.SortFunc(out, func(a, b wireOperation) int { return cmp.Compare(a.key, b.key) })
	return out
}

// openAPIShapes projects one document onto the declarations a client is built against: one
// record per parameter, request body, media type, response header and schema node. Paths and
// media types are visited in a fixed order, so the address an analysis stops at is the same
// address every run stops at. An operation with no identity contributes nothing: it is not
// something a generated client can call, and the document that carries it refuses elsewhere.
func openAPIShapes(doc map[string]any) wireAnalysis {
	w := newWireWalk(doc)
	paths := wireMap(doc["paths"])
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		item, _ := paths[path].(map[string]any)
		for _, verb := range [...]string{"get", "put", "post", "delete", "patch", "head", "options", "trace"} {
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
				w.shapes[id+" "+at] = wireSignature(named)
				if required, _ := named["required"].(bool); required {
					w.shapes[id+" "+at+" requires "+name] = "required"
				}
				w.walk(id, at, wireSchema(named), 0)
			}
			w.walkBody(id, route)
			for status, rawResponse := range wireMap(route["responses"]) {
				response, _ := rawResponse.(map[string]any)
				for name, header := range wireList(response["headers"]) {
					named, _ := header.(map[string]any)
					w.shapes[id+" response "+status+" header/"+name] = wireSignature(named)
				}
				content := wireMap(response["content"])
				for _, media := range slices.Sorted(maps.Keys(content)) {
					at := "response " + status + " " + media
					mediaShape, _ := content[media].(map[string]any)
					schema := wireSchema(mediaShape)
					w.shapes[id+" "+at] = wireSignature(schema)
					w.walk(id, at, schema, 0)
				}
			}
		}
	}
	return w.analysis()
}

// walkBody projects one operation's request body: that a body exists at all, whether the
// operation demands one, and the shape of each media type it accepts. A body's own
// requirement is the same statement a required parameter makes, so it takes a record of the
// same shape and the rule that refuses a new demand reads both.
func (w *wireWalk) walkBody(id string, route map[string]any) {
	body, ok := route["requestBody"].(map[string]any)
	if !ok {
		return
	}
	request := "request body"
	w.shapes[id+" "+request] = "body"
	if required, _ := body["required"].(bool); required {
		w.shapes[id+" "+request+" requires body"] = "required"
	}
	content := wireMap(body["content"])
	for _, media := range slices.Sorted(maps.Keys(content)) {
		at := request + " " + media
		mediaShape, _ := content[media].(map[string]any)
		schema := wireSchema(mediaShape)
		w.shapes[id+" "+at] = wireSignature(schema)
		w.walk(id, at, schema, 0)
	}
}
