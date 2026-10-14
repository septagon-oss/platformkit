package app

// One manifest, two spellings, one answer.
//
// 9a9aa1b returned `Module.Events` to main's `[]string` and added
// `Module.Declared` beside it, on the word that "the two spellings of a
// manifest's event list cannot drift into two answers about what a composition
// emits", and that a bare name is "listed as uncovered rather than pretended
// to be". This file holds those two sentences as behaviour rather than as a
// commit body: the two spellings render the same document, and the spelling
// that cannot describe a payload says so in the document instead of inventing
// one.
//
// The mutation that kills it: any reader of `Module.Events` that is not
// `Module.Emits`, or a generator that gives an undescribed event a
// `"type":"object"` with no `properties` — a document that still validates and
// constrains nothing, which is the failure an integrator's validator cannot
// see.

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

type entry struct {
	EntryID string `json:"entryId"`
}

// channelPayload is the message body's contract as an integrator reads it:
// channels.<name>.messages.<name>.payload.
func channelPayload(t *testing.T, body []byte, name string) any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	channels, _ := doc["channels"].(map[string]any)
	channel, _ := channels[name].(map[string]any)
	messages, _ := channel["messages"].(map[string]any)
	message, _ := messages[name].(map[string]any)
	contract, ok := message["payload"]
	if !ok {
		t.Fatalf("channels.%s.messages.%s has no payload member: %s", name, name, body)
	}
	return contract
}

func TestTheTwoSpellingsOfAManifestsEventListRenderOneDocument(t *testing.T) {
	const name = "ledger.entry_posted"

	bare, err := AsyncAPI([]module.Module{{Name: "ledger", Events: []string{name}}})
	if err != nil {
		t.Fatalf("AsyncAPI with a bare name: %v", err)
	}
	typed, err := AsyncAPI([]module.Module{{Name: "ledger", Declared: []events.Declared{{Name: name}}}})
	if err != nil {
		t.Fatalf("AsyncAPI with the same event as a declaration with no payload: %v", err)
	}
	if !bytes.Equal(bare, typed) {
		t.Errorf("the two spellings of one event list rendered two documents:\n%s\n----\n%s", bare, typed)
	}

	// The bare name is described as what it is: JSON Schema's "anything", and
	// named in the document's own list of events nothing describes.
	if got := channelPayload(t, bare, name); got != true {
		t.Errorf("channels.%s's payload is %v, want JSON Schema's true: a manifest that named no"+
			" payload type must not be handed a schema it did not write", name, got)
	}
	var doc map[string]any
	if err := json.Unmarshal(bare, &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	list, _ := doc["x-uncovered-events"].([]any)
	if len(list) != 1 || list[0] != name {
		t.Errorf("x-uncovered-events is %v, want exactly [%s]: the honest unknown has to be knowable", list, name)
	}

	// A name written both ways is one event, and the typed spelling describes
	// it. Were the bare name to win, the document would open a payload the
	// composition does check at the outbox.
	both, err := AsyncAPI([]module.Module{{Name: "ledger",
		Events:   []string{name},
		Declared: []events.Declared{events.Declare[entry](name)},
	}})
	if err != nil {
		t.Fatalf("AsyncAPI with one event named twice: %v", err)
	}
	contract, _ := channelPayload(t, both, name).(map[string]any)
	props, _ := contract["properties"].(map[string]any)
	if props["entryId"] == nil {
		t.Errorf("an event named in both fields rendered %v: the declared payload describes it", contract)
	}
	var bothDoc map[string]any
	if err := json.Unmarshal(both, &bothDoc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	if list, ok := bothDoc["x-uncovered-events"].([]any); ok && len(list) > 0 {
		t.Errorf("an event whose payload the manifest declared is still listed uncovered: %v", list)
	}
}
