package transport_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The wire shape this file used to assert by hand — {"id","name","tenantId",
// "payload","at","actor"} — is the private form the CloudEvents envelope
// replaced. It is not lost: testdata/legacy.json holds those bytes and
// cloudevents_test.go's TestTheLegacyShapeStillReads reads them back field by
// field, which is the assertion that matters during the rolling window. What
// replaces the byte assertion on the way out is
// TestMarshalWritesTheCloudEventsEnvelope, and its bytes are in a fixture
// rather than a literal so the record of the wire is a file an integrator can
// hand to somebody outside this repository.

func TestNamesKeepTheManifestAndPublisherGrammar(t *testing.T) {
	for name, want := range map[string]bool{"notes.created": true, "notes_v2.item.updated": true,
		"": false, "created": false, "Notes.created": false, "notes.*": false, "notes..created": false} {
		if got := transport.ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
