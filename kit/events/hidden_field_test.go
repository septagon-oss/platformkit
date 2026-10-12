package events_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// hiddenMember is a payload type with one member the kernel fills in on a read
// path and no publisher ever promises — the shape entity.Base's `_i18n` has, and
// it carries the pair of tags that says so: hidden from the REST document, and
// read-only on top of that. Beside it sits one hidden all by itself, the shape the
// diff member `changes` has: kept out of the REST document because no response
// body holds it, and written into every payload the CRUD door publishes.
type hiddenMember struct {
	ID     uuid.UUID `json:"id"`
	Shown  *string   `json:"shown,omitempty" hidden:"true" readOnly:"true"`
	Diff   []string  `json:"diff,omitempty" hidden:"true"`
	Inline string    `json:"inline"`
}

// TestAHiddenMemberIsInTheWireAndNotInTheContract: hidden is huma's own tag,
// the one that keeps a member out of the OpenAPI document, and this projection
// honours it for the same reason — but only beside readOnly, because that pair is
// what says "no publisher writes this". A projection that read only `json:` would put
// every read-side member of entity.Base into the payload schema of every event
// whose payload embeds an entity — a promise about text no publisher ever
// writes, in the contract every subscriber is handed. encoding/json reads no
// such tag, so the member still marshals; the projection simply does not
// promise it, and additionalProperties stays open, which is what makes hiding
// it safe rather than lossy.
//
// `diff` is the other half of the rule, and the half this repository's own
// contract depends on: kit/events/change.go hides `changes` because no REST body
// ever carries it, and the door writes it into the payload it publishes. A
// projection that honoured hidden alone would delete a promise the committed
// AsyncAPI document already makes — which that document's own wire gate refuses
// as breaking.
func TestAHiddenMemberIsInTheWireAndNotInTheContract(t *testing.T) {
	schema := events.Declare[*hiddenMember]("events_test.hidden_member").Schema()
	if schema == nil {
		t.Fatal("the payload type projected to nothing")
	}
	names := map[string]bool{}
	for _, p := range schema.Properties {
		names[p.Name] = true
	}
	if names["shown"] {
		t.Error("the payload schema promises `shown`, which hidden beside readOnly keeps out of the contract")
	}
	if !names["diff"] {
		t.Error("the payload schema dropped `diff`, which is hidden from the REST document and written into the event: the tag alone is no reason to omit it")
	}
	if !names["inline"] || !names["id"] {
		t.Errorf("the projection lost a member beside the hidden one: %v", names)
	}

	// And the wire still carries it, which is the half that makes the omission
	// a description of the contract and not a restriction on the bytes.
	shown := "en"
	raw, err := json.Marshal(&hiddenMember{ID: uuid.New(), Shown: &shown, Inline: "x"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := wire["shown"]; !ok {
		t.Error("encoding/json dropped the hidden member, so the projection would be refusing a member publishers do write")
	}
}
