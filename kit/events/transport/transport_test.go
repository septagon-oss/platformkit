package transport_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestEnvelopeKeepsItsExistingWireIdentity(t *testing.T) {
	id := uuid.MustParse("6a123a70-01b9-4b96-9bc2-1aa316200532")
	event := transport.Event{ID: id, Name: "notes.created", TenantID: id,
		Payload: json.RawMessage(`{"title":"Draft"}`), At: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"6a123a70-01b9-4b96-9bc2-1aa316200532","name":"notes.created","tenantId":"6a123a70-01b9-4b96-9bc2-1aa316200532","payload":{"title":"Draft"},"at":"2026-09-13T12:00:00Z","actor":"00000000-0000-0000-0000-000000000000"}`
	if string(encoded) != want {
		t.Fatalf("event wire shape = %s, want %s", encoded, want)
	}
	var decoded transport.Event
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(event, decoded) {
		t.Fatalf("wire round trip lost event facts: %+v %v", decoded, err)
	}
}

func TestNamesKeepTheManifestAndPublisherGrammar(t *testing.T) {
	for name, want := range map[string]bool{"notes.created": true, "notes_v2.item.updated": true,
		"": false, "created": false, "Notes.created": false, "notes.*": false, "notes..created": false} {
		if got := transport.ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
