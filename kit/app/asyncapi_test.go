package app

// asyncapi_test.go checks the document against the fields AsyncAPI names, by
// name, from the decoded bytes. It deliberately does not unmarshal into the
// structs that wrote it: a projection validated against its own types proves the
// encoder ran, not that the document says what a reader looks for. A real
// validator is a dependency this repository will not add for a file nothing runs.

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
)

// asyncAPIFixture is the smallest composition with something to say: one module
// that emits two events — one whose payload the manifest typed and one it did
// not — and one that handles both.
func asyncAPIFixture(transport string) Description {
	return Description{
		DescribeVersion: DescribeVersion,
		Transport:       transport,
		Modules: []ModuleDescription{
			{Name: "task", Events: []EventDescription{
				{Name: "task.assigned", Schema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`)},
				{Name: "task.untyped"},
			}},
			{Name: "audit", Subscriptions: []string{"task.assigned", "task.untyped"}},
		},
	}
}

func TestAsyncAPIProjectsTheComposition(t *testing.T) {
	// Two endpoints, because nats.url admits a cluster and the document has one
	// server to name it with.
	raw, err := AsyncAPI(asyncAPIFixture("jetstream"), "nats://broker.example:4222,nats://other.example:4222")
	if err != nil {
		t.Fatalf("AsyncAPI: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	// The sections come out in the dialect's order rather than encoding/json's
	// alphabetical sort of a map.
	want := []string{"asyncapi", "info", "servers", "channels", "components", "operations"}
	if got := jsonKeys(raw); !slices.Equal(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
	if doc["asyncapi"] != "3.0.0" {
		t.Errorf("asyncapi = %v, want the 3.0.0 dialect", doc["asyncapi"])
	}

	info, _ := doc["info"].(map[string]any)
	if info["title"] != "PlatformKit" || info["version"] != strconv.Itoa(DescribeVersion) {
		t.Errorf("info = %v, want the title PlatformKit and the description's version", info)
	}

	nats, _ := doc["servers"].(map[string]any)["nats"].(map[string]any)
	if nats["host"] != "broker.example:4222,other.example:4222" || nats["protocol"] != "nats" {
		t.Errorf("servers.nats = %v, want the nats.url endpoints without their scheme and protocol nats", nats)
	}

	members := jsonMembers(raw)
	for _, section := range []string{"channels", "operations"} {
		if keys := jsonKeys(members[section]); !slices.IsSorted(keys) {
			t.Errorf("%s is not sorted by name: %v", section, keys)
		}
	}

	channels, _ := doc["channels"].(map[string]any)
	task, _ := channels["task.assigned"].(map[string]any)
	if task == nil {
		t.Fatalf("no channel keyed task.assigned: %v", channels)
	}
	// The address is the subject the broker publishes on, scheme and all.
	if task["address"] != "platformkit.task.assigned" {
		t.Errorf("address = %v, want platformkit.task.assigned, the subject the NATS provider publishes on", task["address"])
	}
	msgs, _ := task["messages"].(map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("channel carries %d messages, want the one reference: %v", len(msgs), task["messages"])
	}
	ref, _ := msgs["task-assigned"].(map[string]any)
	if ref["$ref"] != "#/components/messages/task-assigned" {
		t.Errorf("message reference = %v", ref["$ref"])
	}

	components, _ := doc["components"].(map[string]any)
	messages, _ := components["messages"].(map[string]any)
	assigned, _ := messages["task-assigned"].(map[string]any)
	if assigned["contentType"] != "application/json" {
		t.Errorf("contentType = %v", assigned["contentType"])
	}
	if _, typed := assigned["headers"]; typed {
		t.Error("the message carries headers; the transport puts nothing in a NATS header")
	}
	payload, _ := assigned["payload"].(map[string]any)
	steps, _ := payload["allOf"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["$ref"] != "#/components/schemas/cloudevent" {
		t.Fatalf("payload does not reference the one envelope schema: %v", payload["allOf"])
	}
	data, _ := payload["properties"].(map[string]any)["data"].(map[string]any)
	if _, ok := data["properties"].(map[string]any)["taskId"]; !ok {
		t.Errorf("data is not the declared payload schema: %v", data)
	}
	// An event nobody typed is still an envelope; what is unknown is its data.
	untyped, _ := messages["task-untyped"].(map[string]any)["payload"].(map[string]any)
	if untyped["properties"].(map[string]any)["data"] != true {
		t.Errorf("an undeclared payload pinned data to %v, want JSON Schema's anything", untyped["properties"])
	}

	schemas, _ := components["schemas"].(map[string]any)
	envelopeSchema, _ := schemas["cloudevent"].(map[string]any)
	if envelopeSchema == nil {
		t.Fatalf("no components.schemas.cloudevent to reference: %v", schemas)
	}
	required, _ := envelopeSchema["required"].([]any)
	for _, member := range []string{"specversion", "id", "source", "type", "time", "tenantid"} {
		if !slices.ContainsFunc(required, func(v any) bool { return v == member }) {
			t.Errorf("the envelope does not require %q, which transport always writes", member)
		}
	}
	props, _ := envelopeSchema["properties"].(map[string]any)
	if props["data"] != true {
		t.Errorf("the envelope pins data to %v; each message pins its own", props["data"])
	}

	ops, _ := doc["operations"].(map[string]any)
	if len(ops) != 4 {
		t.Errorf("%d operations, want one send and one receive per event: %v", len(ops), jsonKeyList(ops))
	}
	for _, pair := range []struct{ key, action, summary string }{
		{"task.send.task.assigned", "send", "task publishes task.assigned"},
		{"audit.receive.task.assigned", "receive", "audit handles task.assigned"},
	} {
		op, ok := ops[pair.key].(map[string]any)
		if !ok {
			t.Errorf("no operation %q in %v", pair.key, jsonKeyList(ops))
			continue
		}
		if op["action"] != pair.action || op["summary"] != pair.summary {
			t.Errorf("operation %q = %v, want action %q and summary %q", pair.key, op, pair.action, pair.summary)
		}
		if ch, _ := op["channel"].(map[string]any); ch["$ref"] != "#/channels/task.assigned" {
			t.Errorf("operation %q refers to channel %v", pair.key, ch)
		}
	}
}

// TestAsyncAPINamesNoBrokerForAnInProcessTransport: the server entry is a claim
// about the deployment, and role all with the memory transport is a deployment
// with no broker in it.
func TestAsyncAPINamesNoBrokerForAnInProcessTransport(t *testing.T) {
	raw, err := AsyncAPI(asyncAPIFixture("memory"), "nats://broker.example:4222")
	if err != nil {
		t.Fatalf("AsyncAPI: %v", err)
	}
	if _, named := jsonMembers(raw)["servers"]; named {
		t.Error(`the document names a "servers" member for the in-memory transport`)
	}
}

// TestAsyncAPIRefusesAnAddressItCannotNameAHostFor keeps a configuration typo out
// of a document somebody will generate a client from.
func TestAsyncAPIRefusesAnAddressItCannotNameAHostFor(t *testing.T) {
	if _, err := AsyncAPI(asyncAPIFixture("jetstream"), "broker.example"); err == nil {
		t.Error("AsyncAPI accepted an endpoint with no scheme and no host")
	}
}

// jsonMembers is an object's members by name, keeping each value's bytes: the way
// to ask a question of a nested object without losing the order it was written in.
func jsonMembers(raw []byte) map[string]json.RawMessage {
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	return members
}

// jsonKeys is an object's member names in the order they were written, which is
// what map[string]any cannot answer and what makes the document diffable.
func jsonKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var keys []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return keys
		}
		name, ok := key.(string)
		if !ok {
			return keys
		}
		keys = append(keys, name)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return keys
		}
	}
	return keys
}

func jsonKeyList(m map[string]any) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
