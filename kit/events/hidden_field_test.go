package events_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// hiddenMember is a payload type with one member the kernel fills in on a read
// path and no publisher ever promises — the shape entity.Base's `_i18n` has.
type hiddenMember struct {
	ID     uuid.UUID `json:"id"`
	Shown  *string   `json:"shown,omitempty" hidden:"true"`
	Inline string    `json:"inline"`
}

// TestAHiddenMemberIsInTheWireAndNotInTheContract: hidden is huma's own tag,
// the one that keeps a member out of the OpenAPI document, and this projection
// honours it for the same reason. A projection that read only `json:` would put
// every read-side member of entity.Base into the payload schema of every event
// whose payload embeds an entity — a promise about text no publisher ever
// writes, in the contract every subscriber is handed. encoding/json reads no
// such tag, so the member still marshals; the projection simply does not
// promise it, and additionalProperties stays open, which is what makes hiding
// it safe rather than lossy.
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
		t.Error("the payload schema promises `shown`, which the hidden tag keeps out of the contract")
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
