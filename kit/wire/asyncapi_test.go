package wire_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

func TestAsyncStructureAndLinks(t *testing.T) {
	old := baseline("asyncapi")
	for _, tc := range []struct {
		name, rule, path, member string
		change                   func(map[string]any)
	}{
		{"channel removed", "B3", "item.changed", "channel/item", func(doc map[string]any) {
			delete(at(doc, "channels"), "item")
			delete(at(doc, "operations"), "publish")
		}},
		{"message removed", "B3", "item.changed", "channel/item/message/item", func(doc map[string]any) {
			delete(at(doc, "channels", "item", "messages"), "item")
			operation(doc, "asyncapi")["messages"] = []any{}
		}},
		{"message link removed", "B3", "SEND item.changed", "publish message #/channels/item/messages/item", func(doc map[string]any) {
			operation(doc, "asyncapi")["messages"] = []any{}
		}},
		{"action changed", "B2", "SEND item.changed", "route", func(doc map[string]any) { operation(doc, "asyncapi")["action"] = "receive" }},
		{"message name", "B3", "item.changed", "channel/item/message/item", func(doc map[string]any) { at(doc, "channels", "item", "messages", "item")["name"] = "renamed" }},
		{"effective content type", "B3", "item.changed", "channel/item/message/item", func(doc map[string]any) { doc["defaultContentType"] = "text/plain" }},
		{"identity changed", "B1", "SEND item.changed", "operationId", func(doc map[string]any) {
			at(doc, "operations")["renamed"] = operation(doc, "asyncapi")
			delete(at(doc, "operations"), "publish")
		}},
		{"payload gone", "B3", "SEND item.changed", "publish response message/item/item payload", func(doc map[string]any) { delete(at(doc, "channels", "item", "messages", "item"), "payload") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := wire.Compare(old, mutated(t, old, tc.change))
			if !slices.ContainsFunc(got, func(b wire.Break) bool { return b.Rule == tc.rule && b.Path == tc.path && b.Member == tc.member }) {
				t.Fatal(got)
			}
		})
	}
	unreferenced := mutated(t, old, func(doc map[string]any) { doc["operations"] = map[string]any{} })
	removed := mutated(t, unreferenced, func(doc map[string]any) { delete(at(doc, "channels"), "item") })
	got := wire.Compare(unreferenced, removed)
	if len(got) != 2 || got[0].Path != "item.changed" || got[1].Path != "item.changed" {
		t.Fatal(got)
	}
	added := mutated(t, old, func(doc map[string]any) {
		at(doc, "channels", "item", "messages")["extra"] = map[string]any{"payload": map[string]any{"type": "string"}}
		operation(doc, "asyncapi")["messages"] = []any{map[string]any{"$ref": "#/channels/item/messages/item"}, map[string]any{"$ref": "#/channels/item/messages/extra"}}
		at(doc, "channels")["extra"] = map[string]any{"address": "extra", "messages": map[string]any{}}
	})
	if got := wire.Compare(old, added); len(got) != 0 {
		t.Fatal(got)
	}
	headers := mutated(t, old, func(doc map[string]any) {
		at(doc, "channels", "item", "messages", "item")["headers"] = map[string]any{"type": "object", "properties": map[string]any{"trace": map[string]any{"type": "string"}}}
	})
	retyped := mutated(t, headers, func(doc map[string]any) {
		at(doc, "channels", "item", "messages", "item", "headers", "properties", "trace")["type"] = "integer"
	})
	if got := wire.Compare(headers, retyped); len(got) != 1 || got[0].Rule != "B3" || !strings.HasSuffix(got[0].Member, "headers .trace") {
		t.Fatal(got)
	}
}

func TestAsyncMalformedNodesRefuse(t *testing.T) {
	old := baseline("asyncapi")
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"channel", func(doc map[string]any) { at(doc, "channels")["item"] = nil }},
		{"message", func(doc map[string]any) { at(doc, "channels", "item", "messages")["item"] = nil }},
		{"operation", func(doc map[string]any) { at(doc, "operations")["publish"] = nil }},
		{"channel reference", func(doc map[string]any) {
			operation(doc, "asyncapi")["channel"] = map[string]any{"$ref": "#/channels/missing"}
		}},
		{"message reference", func(doc map[string]any) {
			operation(doc, "asyncapi")["messages"] = []any{map[string]any{"$ref": "#/channels/item/messages/missing"}}
		}},
		{"wrong channel message", func(doc map[string]any) {
			operation(doc, "asyncapi")["messages"] = []any{map[string]any{"$ref": "#/channels/input/messages/input"}}
		}},
		{"duplicate address", func(doc map[string]any) { at(doc, "operations")["duplicate"] = operation(doc, "asyncapi") }},
		{"cyclic message", func(doc map[string]any) {
			at(doc, "channels", "item", "messages")["item"] = map[string]any{"$ref": "#/channels/item/messages/item"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := wire.Compare(old, mutated(t, old, tc.change))
			if len(got) != 1 || got[0].Path != "$" || got[0].Rule != "B3" {
				t.Fatal(got)
			}
		})
	}
}

func TestLocalPointersAndCycles(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			old := mutated(t, baseline(kind), func(doc map[string]any) {
				shape(doc, kind, "response")["$ref"] = "#/components/schemas/a~1b~0c"
				doc["components"] = map[string]any{"schemas": map[string]any{"a/b~c": map[string]any{"type": "object", "properties": map[string]any{
					"linked": map[string]any{"type": "string"}, "recursive": map[string]any{"$ref": "#/components/schemas/a~1b~0c"},
				}}}}
			})
			if got := wire.Compare(old, old); len(got) != 0 {
				t.Fatal(got)
			}
			current := mutated(t, old, func(doc map[string]any) {
				at(doc, "components", "schemas", "a/b~c", "properties", "linked")["type"] = "integer"
			})
			if got := wire.Compare(old, current); len(got) == 0 || got[0].Rule != "B3" || !strings.HasSuffix(got[0].Member, ".linked") {
				t.Fatal(got)
			}
		})
	}
	old := mutated(t, baseline("asyncapi"), func(doc map[string]any) {
		channels := at(doc, "channels")
		channels["a/b~c"] = channels["item"]
		delete(channels, "item")
		operation(doc, "asyncapi")["channel"] = map[string]any{"$ref": "#/channels/a~1b~0c"}
		operation(doc, "asyncapi")["messages"] = []any{map[string]any{"$ref": "#/channels/a~1b~0c/messages/item"}}
	})
	current := mutated(t, old, func(doc map[string]any) {
		at(doc, "channels", "a/b~c", "messages", "item", "payload", "properties", "name")["type"] = "integer"
	})
	if got := wire.Compare(old, current); len(got) != 1 || got[0].Rule != "B3" || got[0].Path != "SEND item.changed" {
		t.Fatal(got)
	}
}

func TestPointerThroughArray(t *testing.T) {
	old := []byte(`{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/choice/anyOf/0"}}}}}}}},"components":{"schemas":{"choice":{"anyOf":[{"type":"object","properties":{"value":{"type":"string"}}}]}}}}`)
	if got := wire.Compare(old, old); len(got) != 0 {
		t.Fatal(got)
	}
	current := []byte(strings.Replace(string(old), `"value":{"type":"string"}`, `"value":{"type":"integer"}`, 1))
	if got := wire.Compare(old, current); len(got) != 1 || got[0].Rule != "B3" || !strings.HasSuffix(got[0].Member, ".value") {
		t.Fatal(got)
	}
}
