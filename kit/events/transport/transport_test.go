package transport_test

import (
	"slices"
	"strings"
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

// TestTheFilterSetAnswersThisBuildsAddressAndThePreviousOnes holds the rule the
// rolling window rests on, written as literals rather than as a call compared to
// itself: a subscription to one event name answers two subjects, and the second
// is the address a publisher on the previous build writes. It is a unit test
// because the failure is arithmetic — a NATS `*` matches exactly one token, so
// the four-token filter cannot reach the three-token message, the stream keeps
// the message, the relay stamps the row published and no handler ever runs.
// Nothing in this repository writes that old address; only a process still
// running the previous build does, which is why the assertion has to be here
// rather than only in a broker fixture.
func TestTheFilterSetAnswersThisBuildsAddressAndThePreviousOnes(t *testing.T) {
	const name = "ledger.invoice_issued"
	want := []string{"platformkit.*." + name, "platformkit." + name}
	if got := transport.Filters(name); !slices.Equal(got, want) {
		t.Errorf("Filters(%q) = %v, want %v", name, got, want)
	}
	// Why two filters and not one wildcard: the old subject has one fewer token
	// than the filter, and `*` neither spans nor skips a token.
	if old, filter := strings.Count("platformkit."+name, "."), strings.Count(transport.Filter(name), "."); old >= filter {
		t.Errorf("%q has %d dots and %q has %d, so one filter would reach the old address and the second is dead weight",
			"platformkit."+name, old, transport.Filter(name), filter)
	}
}
