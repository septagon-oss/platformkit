package wire

import (
	"cmp"
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

func openAPIShapes(doc map[string]any) map[string]string {
	out := map[string]string{}
	for _, raw := range wireMap(doc["paths"]) {
		item, _ := raw.(map[string]any)
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
