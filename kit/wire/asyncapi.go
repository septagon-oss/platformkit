package wire

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func pointerPart(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// AsyncAPI channel/message references may point at a component; unlike schema
// recursion, a cycle here cannot produce an operation or message object.
func asyncObject(doc map[string]any, raw any) (map[string]any, error) {
	node, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AsyncAPI node must be an object")
	}
	seen := map[string]bool{}
	for {
		rawRef, present := node["$ref"]
		if !present {
			return node, nil
		}
		ref, _ := rawRef.(string)
		node = wireResolve(doc, ref)
		if node == nil || seen[ref] {
			return nil, fmt.Errorf("unresolved or cyclic local reference %q", ref)
		}
		seen[ref] = true
	}
}

func validateAsyncAPI(doc map[string]any) error {
	channels := wireMap(doc["channels"])
	for _, key := range slices.Sorted(maps.Keys(channels)) {
		channel, err := asyncObject(doc, channels[key])
		if err != nil {
			return err
		}
		if address, _ := channel["address"].(string); address == "" {
			return fmt.Errorf("channel %q has no address", key)
		}
		messages, ok := channel["messages"].(map[string]any)
		if !ok {
			return fmt.Errorf("channel %q messages must be an object", key)
		}
		for _, name := range slices.Sorted(maps.Keys(messages)) {
			msg, err := asyncObject(doc, messages[name])
			if err != nil {
				return err
			}
			for _, field := range []string{"payload", "headers"} {
				if raw, present := msg[field]; present {
					if err := validateSchema(doc, raw, map[string]bool{}); err != nil {
						return err
					}
				}
			}
		}
	}
	operations := wireMap(doc["operations"])
	for _, id := range slices.Sorted(maps.Keys(operations)) {
		op, ok := operations[id].(map[string]any)
		if !ok {
			return fmt.Errorf("operation %q must be an object", id)
		}
		if op["action"] != "send" && op["action"] != "receive" {
			return fmt.Errorf("operation %q must send or receive", id)
		}
		ref, _ := wireMap(op["channel"])["$ref"].(string)
		key, err := asyncChannelKey(doc, ref)
		if err != nil {
			return err
		}
		refs, ok := op["messages"].([]any)
		if !ok {
			return fmt.Errorf("operation %q messages must be an array", id)
		}
		for _, raw := range refs {
			ref, _ := wireMap(raw)["$ref"].(string)
			if _, err := asyncMessageKey(doc, key, ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func asyncChannelKey(doc map[string]any, ref string) (string, error) {
	for key := range wireMap(doc["channels"]) {
		if ref == "#/channels/"+pointerPart(key) && wireResolve(doc, ref) != nil {
			return key, nil
		}
	}
	return "", fmt.Errorf("unresolved channel reference %q", ref)
}

func asyncMessageKey(doc map[string]any, channelKey, ref string) (string, error) {
	channel, _ := asyncObject(doc, wireMap(doc["channels"])[channelKey])
	for key := range wireMap(channel["messages"]) {
		if ref == "#/channels/"+pointerPart(channelKey)+"/messages/"+pointerPart(key) && wireResolve(doc, ref) != nil {
			return key, nil
		}
	}
	return "", fmt.Errorf("unresolved message reference %q in channel %q", ref, channelKey)
}

func asyncAPIOperations(doc map[string]any) wireRoutes {
	var out wireRoutes
	for id, raw := range wireMap(doc["operations"]) {
		op := wireMap(raw)
		ref, _ := wireMap(op["channel"])["$ref"].(string)
		channel, _ := asyncObject(doc, wireResolve(doc, ref))
		action, _ := op["action"].(string)
		address, _ := channel["address"].(string)
		out = append(out, wireOperation{strings.ToUpper(action) + " " + address, id, wireAuth(op)})
	}
	slices.SortFunc(out, func(a, b wireOperation) int { return cmp.Compare(a.key, b.key) })
	return out
}

func asyncAPIShapes(doc map[string]any) map[string]string {
	out := map[string]string{}
	for key, raw := range wireMap(doc["channels"]) {
		channel, _ := asyncObject(doc, raw)
		at := "channel/" + pointerPart(key)
		out[at], _ = channel["address"].(string)
		for name, raw := range wireMap(channel["messages"]) {
			msg, _ := asyncObject(doc, raw)
			content := msg["contentType"]
			if content == nil {
				content = doc["defaultContentType"]
			}
			signature, _ := json.Marshal([]any{msg["name"], content})
			out[at+"/message/"+pointerPart(name)] = string(signature)
		}
	}
	for id, raw := range wireMap(doc["operations"]) {
		op := wireMap(raw)
		ref, _ := wireMap(op["channel"])["$ref"].(string)
		channelKey, _ := asyncChannelKey(doc, ref)
		channel, _ := asyncObject(doc, wireMap(doc["channels"])[channelKey])
		out[id+" channel"] = ref
		direction := "response"
		if op["action"] == "receive" {
			direction = "request"
		}
		for _, raw := range op["messages"].([]any) {
			ref, _ := wireMap(raw)["$ref"].(string)
			name, _ := asyncMessageKey(doc, channelKey, ref)
			out[id+" message "+ref] = "message"
			msg, _ := asyncObject(doc, wireMap(channel["messages"])[name])
			for _, field := range []string{"payload", "headers"} {
				if schema, ok := msg[field].(map[string]any); ok {
					at := direction + " message/" + pointerPart(channelKey) + "/" + pointerPart(name) + " " + field
					walkWire(out, id, at, schema, doc, 0)
				}
			}
		}
	}
	return out
}

func asyncStructuralPath(doc map[string]any, member string) string {
	for key, raw := range wireMap(doc["channels"]) {
		prefix := "channel/" + pointerPart(key)
		if member == prefix || strings.HasPrefix(member, prefix+"/message/") {
			channel, _ := asyncObject(doc, raw)
			address, _ := channel["address"].(string)
			return address
		}
	}
	return ""
}
