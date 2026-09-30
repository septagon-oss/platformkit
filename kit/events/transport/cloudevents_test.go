package transport_test

// cloudevents_test.go is the gate on the wire form. testdata/cloudevent.json is
// the byte-for-byte record of what this package writes and testdata/legacy.json
// the record of what a previous build wrote; both are read here rather than
// restated, so a change to the envelope fails a test that names the file.

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// fixed is one event, used by every case here so the two fixtures describe the
// same facts. Its tenant is its own id only because that is shorter to read.
func fixed() transport.Event {
	id := uuid.MustParse("6a123a70-01b9-4b96-9bc2-1aa316200532")
	return transport.Event{
		ID: id, Name: "notes.created", TenantID: id,
		Payload: json.RawMessage(`{"title":"Draft"}`),
		At:      time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(body)
}

func TestMarshalWritesTheCloudEventsEnvelope(t *testing.T) {
	ev := fixed()
	ev.TraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ev.TraceState = "rojo=00f067aa0ba902b7"

	encoded, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if want := fixture(t, "cloudevent.json"); !bytes.Equal(encoded, want) {
		t.Errorf("the envelope changed from testdata/cloudevent.json:\n got %s\nwant %s", encoded, want)
	}

	// The required context attributes are present and say what the fields say.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"specversion", "id", "source", "type", "subject", "time", "datacontenttype"} {
		if _, ok := doc[attr]; !ok {
			t.Errorf("the envelope carries no %s", attr)
		}
	}
	if got := string(doc["specversion"]); got != `"1.0"` {
		t.Errorf("specversion = %s", got)
	}
	// The tenant is in the address, not only in the body: that is what lets a
	// durable be per tenant (decision 0053 §1).
	if got, want := string(doc["subject"]), `"platformkit.`+ev.TenantID.String()+`.notes.created"`; got != want {
		t.Errorf("subject = %s, want %s", got, want)
	}
	if got, want := string(doc["tenantid"]), `"`+ev.TenantID.String()+`"`; got != want {
		t.Errorf("tenantid = %s, want %s", got, want)
	}
	// An actor that does not exist is absent, not the nil UUID.
	if _, ok := doc["actor"]; ok {
		t.Errorf("an event nobody caused carries actor %s; absent is the answer", doc["actor"])
	}
}

func TestRoundTripKeepsEveryFact(t *testing.T) {
	ev := fixed()
	ev.Actor = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ev.TraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ev.TraceState = "rojo=00f067aa0ba902b7"

	encoded, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var got transport.Event
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("the envelope this package wrote does not read back: %v", err)
	}
	if !reflect.DeepEqual(ev, got) {
		t.Errorf("round trip lost event facts:\n got %+v\nwant %+v", got, ev)
	}
}

// TestTheLegacyShapeStillReads covers the rolling window, not a leftover: while
// a web role on the previous build publishes, a worker on this one must be able
// to read it. This package never writes that shape — the test above is the gate
// on what it does write.
func TestTheLegacyShapeStillReads(t *testing.T) {
	body := fixture(t, "legacy.json")
	if bytes.Contains(body, []byte("specversion")) {
		t.Fatal("testdata/legacy.json is no longer the pre-envelope shape")
	}
	var got transport.Event
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the shape a previous build published no longer reads: %v", err)
	}
	if want := fixed(); got.ID != want.ID || got.Name != want.Name || got.TenantID != want.TenantID ||
		!bytes.Equal(got.Payload, want.Payload) || !got.At.Equal(want.At) {
		t.Errorf("legacy decode lost event facts: got %+v", got)
	}
	if got.TraceParent != "" {
		t.Errorf("a legacy event carries traceparent %q it cannot have had", got.TraceParent)
	}
}

func TestAnEnvelopeThatIsNotOneIsRefused(t *testing.T) {
	good := fixed()
	encoded, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	base := string(encoded)

	for _, tc := range []struct {
		name, want string
		body       string
	}{
		{"null is not an event", "a JSON object", `null`},
		{"an array is not an event", "a JSON object", `[1,2]`},
		{"a pre-envelope document with no tenant", "no tenant", `{"type":"notes.created"}`},
		{"another specversion", "specversion", `{"specversion":"1.1","type":"notes.created"}`},
		{"no type", "no type", `{"specversion":"1.0","tenantid":"6a123a70-01b9-4b96-9bc2-1aa316200532"}`},
		{"no tenant is undeliverable", "tenantid", `{"specversion":"1.0","id":"6a123a70-01b9-4b96-9bc2-1aa316200532","source":"/notes","type":"notes.created","subject":"platformkit.6a123a70-01b9-4b96-9bc2-1aa316200532.notes.created","time":"2026-09-13T12:00:00Z","datacontenttype":"application/json"}`},
		{"a foreign source", "source", strings.Replace(base, `"source":"/notes"`, `"source":"/other"`, 1)},
		{"a subject from another tenant", "subject", strings.Replace(base, good.TenantID.String()+".notes.created", uuid.NewString()+".notes.created", 1)},
		{"no time", "time", strings.Replace(base, `"time":"2026-09-13T12:00:00Z",`, "", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ev transport.Event
			err := json.Unmarshal([]byte(tc.body), &ev)
			if err == nil {
				t.Fatalf("accepted %s: %+v", tc.name, ev)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

// TestTheSubjectIsTheAddress the broker uses. The envelope and the transport
// are two readers of one function; a subject a bridge could subscribe to and a
// subject the publisher used must be the same string.
func TestTheSubjectIsTheAddress(t *testing.T) {
	tenant, name := uuid.New(), "user.invited"
	sub := transport.Subject(tenant, name)
	if !strings.HasPrefix(sub, transport.SubjectPrefix+"."+tenant.String()+".") {
		t.Errorf("Subject(%s, %q) = %q", tenant, name, sub)
	}
	// The wildcard a subscription filters matches one event across tenants and
	// nothing else.
	filter := transport.Filter(name)
	for _, other := range []string{sub, transport.Subject(uuid.New(), name)} {
		if !matches(filter, other) {
			t.Errorf("filter %q does not match %q", filter, other)
		}
	}
	for _, other := range []string{transport.Subject(tenant, "user.removed"), "platformkit." + tenant.String()} {
		if matches(filter, other) {
			t.Errorf("filter %q matches %q, which it must not", filter, other)
		}
	}
}

// matches is NATS' own wildcard rule, kept to the two tokens this scheme uses.
func matches(filter, subject string) bool {
	f, s := strings.Split(filter, "."), strings.Split(subject, ".")
	if len(f) != len(s) {
		return false
	}
	for i := range f {
		if f[i] != "*" && f[i] != ">" && f[i] != s[i] {
			return false
		}
	}
	return true
}
