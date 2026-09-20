package transport_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// goldenEvent is the event every wire-format case reads: fully populated, so each
// member of the CloudEvents envelope is present and named, with an actor that is
// not the nil UUID and a timestamp whose fraction is long enough to show that
// nothing truncated it.
func goldenEvent() transport.Event {
	return transport.Event{
		ID:       uuid.MustParse("6a123a70-01b9-4b96-9bc2-1aa316200532"),
		Name:     "notes.created",
		TenantID: uuid.MustParse("b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"),
		Payload:  json.RawMessage(`{"title":"Draft"}`),
		At:       time.Date(2026, 9, 13, 12, 0, 0, 123456789, time.UTC),
		Actor:    uuid.MustParse("11111111-2222-3333-4444-555555555555"),
	}
}

// fixture is a committed document rather than one written here, so a change to
// what this package emits is a change to a file a reviewer sees and diffs, and
// the legacy one is what a previous build really published rather than what this
// build remembers. testdata/cloudevent.json carries no trailing newline: the
// comparison below is byte for byte and encoding/json writes none.
func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %s", name, err)
	}
	return string(body)
}

// TestEnvelopeIsAGoldenCloudEvent pins the whole envelope: the members, their
// order, and the two extension attributes.
func TestEnvelopeIsAGoldenCloudEvent(t *testing.T) {
	encoded, err := json.Marshal(goldenEvent())
	if err != nil {
		t.Fatal(err)
	}
	if want := fixture(t, "cloudevent.json"); string(encoded) != want {
		t.Fatalf("wire envelope =\n%s\nwant\n%s", encoded, want)
	}
}

func TestEnvelopeRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		what    string
		ev      transport.Event
		omitted []string
	}{
		{"fully populated", goldenEvent(), nil},
		// The relay reads a null actor column as the nil UUID, and an event whose
		// publisher marshalled nothing has no payload at all: both are ordinary,
		// and neither is allowed to come back as a different event.
		{"no actor, no payload", transport.Event{
			ID:       uuid.MustParse("6a123a70-01b9-4b96-9bc2-1aa316200532"),
			Name:     "task.assigned",
			TenantID: uuid.MustParse("b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"),
			At:       time.Date(2026, 9, 13, 12, 0, 0, 123456789, time.UTC),
		}, []string{"actor", "data"}},
	} {
		body, err := json.Marshal(tc.ev)
		if err != nil {
			t.Fatalf("%s: marshal: %s", tc.what, err)
		}
		var got transport.Event
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%s: unmarshal %s: %s", tc.what, body, err)
		}
		if !reflect.DeepEqual(tc.ev, got) {
			t.Errorf("%s: round trip =\n%+v\nwant\n%+v", tc.what, got, tc.ev)
		}
		// The nil actor and the empty payload have to leave the document rather
		// than arrive in it as a zero: an empty data decodes to a JSON null
		// payload, and an actor of all zeros names a user who does not exist.
		var members map[string]json.RawMessage
		if err := json.Unmarshal(body, &members); err != nil {
			t.Fatalf("%s: the envelope is not an object: %s", tc.what, err)
		}
		for _, member := range tc.omitted {
			if _, ok := members[member]; ok {
				t.Errorf("%s: %s is present; an attribute that does not apply is absent", tc.what, member)
			}
		}
	}
}

// TestPreviousShapeStillDecodes is the rolling window: a web role on the previous
// build writes the old member names and this worker reads them. The old shape is
// still decoded and no longer produced, which is why the rollout order in
// ../README.md puts worker roles before web roles.
func TestPreviousShapeStillDecodes(t *testing.T) {
	var got transport.Event
	if err := json.Unmarshal([]byte(fixture(t, "legacy.json")), &got); err != nil {
		t.Fatalf("the previous shape no longer decodes: %s", err)
	}
	if want := goldenEvent(); !reflect.DeepEqual(want, got) {
		t.Errorf("previous shape =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRefuseADocumentThatIsNotThisCloudEvent(t *testing.T) {
	for _, tc := range []struct{ what, body, want string }{
		{"another specification version",
			`{"specversion":"0.3","id":"6a123a70-01b9-4b96-9bc2-1aa316200532","source":"/notes","type":"notes.created","time":"2026-09-13T12:00:00Z","tenantid":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}`,
			`specversion "0.3"`},
		{"no type",
			`{"specversion":"1.0","id":"6a123a70-01b9-4b96-9bc2-1aa316200532","source":"/notes","time":"2026-09-13T12:00:00Z","tenantid":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}`,
			"no type"},
		// source and type are the same fact written twice, so a bridge that rewrote
		// one of them is an event whose handler would be the wrong module's.
		{"source disagrees with the module of its type",
			`{"specversion":"1.0","id":"6a123a70-01b9-4b96-9bc2-1aa316200532","source":"/task","type":"notes.created","time":"2026-09-13T12:00:00Z","tenantid":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}`,
			`source "/task"`},
		{"id is not a UUID",
			`{"specversion":"1.0","id":"not-a-uuid","source":"/notes","type":"notes.created","time":"2026-09-13T12:00:00Z","tenantid":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}`,
			`id "not-a-uuid"`},
		{"a document that is not an object",
			`["specversion","1.0"]`, "is a JSON object"},
		{"a bare null", `null`, "is a JSON object"},
	} {
		err := json.Unmarshal([]byte(tc.body), new(transport.Event))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one containing %q", tc.what, err, tc.want)
		}
	}
}

// Malformed input is refused, never a panic and never a partial event: these are
// what a truncated write or a foreign producer can leave on a subject.
func TestMalformedInputFails(t *testing.T) {
	for _, body := range []string{`{`, `{"specversion":`, `{"specversion":1.0}`,
		`{"specversion":"1.0","data":`, `"notes.created"`, `   `} {
		if err := json.Unmarshal([]byte(body), new(transport.Event)); err == nil {
			t.Errorf("the malformed %s decoded", body)
		}
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
