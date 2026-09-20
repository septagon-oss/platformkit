package transport

// The wire format is CloudEvents 1.0 in structured content mode: the envelope
// below is what crosses a broker, so a bridge, an event router or an AsyncAPI
// document can read a PlatformKit event without importing this package.
//
// Event stays the programming model. Its Go fields are what modules, the outbox
// and its handlers use, and the outbox stores columns rather than an envelope,
// so only the JSON form changed. Adopting the envelope while every consumer is
// inside this workspace is a JSON change; adopting it later is a migration.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// specVersion is the only CloudEvents version produced here, and the only
	// one accepted: a document claiming another is refused rather than guessed
	// at, because the two differ in ways this decoder cannot enumerate.
	specVersion = "1.0"
	// dataContentType is the only content type a payload can have. Publish
	// marshals its argument with encoding/json and the outbox column is JSONB,
	// so nothing can produce a payload that is not a JSON document.
	dataContentType = "application/json"
)

// envelope is Event's JSON form. Field order is encoding/json's output order,
// and testdata/cloudevent.json is its byte-for-byte record: the required
// context attributes, then data, then PlatformKit's four extension attributes.
// Extension names are lower-case — the specification makes attribute names
// case-insensitive, so lower-case is the spelling every consumer accepts — except
// the two trace members, which the distributed tracing extension fixes as
// traceparent and tracestate and every W3C reader expects verbatim.
type envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            string          `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data,omitempty"`
	TenantID        string          `json:"tenantid"`
	Actor           string          `json:"actor,omitempty"`
	// The two distributed tracing extension attributes the CloudEvents
	// specification names: traceparent and tracestate, spelled exactly as W3C
	// spells them, so a collector or a bridge that understands one understands
	// this. They are absent when the publisher had no trace, which is what a
	// periodic job and a relay always are.
	TraceParent string `json:"traceparent,omitempty"`
	TraceState  string `json:"tracestate,omitempty"`
}

// MarshalJSON writes the CloudEvents form of the event.
func (e Event) MarshalJSON() ([]byte, error) {
	doc := envelope{
		SpecVersion:     specVersion,
		ID:              e.ID.String(),
		Source:          source(e.Name),
		Type:            e.Name,
		Time:            e.At.UTC().Format(time.RFC3339Nano),
		DataContentType: dataContentType,
		Data:            e.Payload,
		TenantID:        e.TenantID.String(),
	}
	// The nil UUID means "nobody caused this" — a periodic job, the relay, a
	// handler reacting to another event. An optional attribute that does not
	// apply is absent rather than present and zero.
	if e.Actor != uuid.Nil {
		doc.Actor = e.Actor.String()
	}
	// Same rule, and the reason is the same: an event with no trace is the
	// normal case, not a trace with an empty parent.
	doc.TraceParent, doc.TraceState = e.TraceParent, e.TraceState
	return json.Marshal(doc)
}

// UnmarshalJSON reads an event back, accepting both the CloudEvents envelope
// and the shape PlatformKit published before adopting it.
//
// The old shape is decoded and never produced. That asymmetry is the rolling
// window, not a leftover: while a web role still running the previous build
// writes events this worker reads, refusing that window would terminate messages
// the relay had already stamped published_at, and the outbox row is gone from the
// pending set either way. kit/events/README.md states the rollout order that
// closes the window; the envelope never writes the old shape, so the window
// closes when the last previous-build publisher is gone.
func (e *Event) UnmarshalJSON(body []byte) error {
	// Checked before any decoding: encoding/json hands an Unmarshaler a JSON null
	// as the four bytes "null" and no error, so an unguarded null document would
	// decode as an event with every member missing. The length test is what makes
	// the byte test safe when there is no body at all.
	if first := bytes.TrimSpace(body); len(first) == 0 || first[0] != '{' {
		return errors.New("events: an event is a JSON object")
	}
	var header struct {
		SpecVersion *string `json:"specversion"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	if header.SpecVersion == nil {
		var old legacyEvent
		if err := json.Unmarshal(body, &old); err != nil {
			return fmt.Errorf("events: %w", err)
		}
		// Field by field rather than a conversion, which is what the two types
		// allowed while they were identical: the shape a previous build published
		// has no trace context to read and no attempt counted, so both are left
		// empty, and the delivery span starts its own trace instead of pretending
		// to know which one the publisher was part of.
		*e = Event{ID: old.ID, Name: old.Name, TenantID: old.TenantID,
			Payload: old.Payload, At: old.At, Actor: old.Actor}
		return nil
	}
	return e.unmarshalCloud(*header.SpecVersion, body)
}

// unmarshalCloud reads the envelope into Event. Which members must be present is
// the mirror of what MarshalJSON writes, with one exception on each side: data
// is optional because an empty payload is omitted, and datacontenttype is not
// read because the payload is kept as the raw JSON bytes it arrived as. tenantid
// is required even though the specification leaves every extension optional —
// without a tenant there is no transaction to deliver the event in, and a
// document that cannot name one is not this program's to deliver.
func (e *Event) unmarshalCloud(version string, body []byte) error {
	if version != specVersion {
		return fmt.Errorf("events: CloudEvents specversion %q is not %q", version, specVersion)
	}
	var doc envelope
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	if doc.Type == "" {
		return errors.New("events: CloudEvents document has no type")
	}
	// source is derived from type on the way out, so checking it here is not
	// trusting a second copy: an event attributed to the wrong module is an
	// event whose handler is the wrong module's, and a bridge that rewrote one
	// of the two and not the other must fail loudly.
	if want := source(doc.Type); doc.Source != want {
		return fmt.Errorf("events: CloudEvents source %q is not the module of type %q, which is %q", doc.Source, doc.Type, want)
	}
	at, err := time.Parse(time.RFC3339Nano, doc.Time)
	if err != nil {
		return fmt.Errorf("events: CloudEvents time %q is not an RFC 3339 timestamp: %w", doc.Time, err)
	}
	id, err := parseUUID("id", doc.ID)
	if err != nil {
		return err
	}
	tenantID, err := parseUUID("tenantid", doc.TenantID)
	if err != nil {
		return err
	}
	actor := uuid.Nil
	if doc.Actor != "" {
		if actor, err = parseUUID("actor", doc.Actor); err != nil {
			return err
		}
	}
	*e = Event{ID: id, Name: doc.Type, TenantID: tenantID, Payload: doc.Data, At: at, Actor: actor,
		TraceParent: doc.TraceParent, TraceState: doc.TraceState}
	return nil
}

// legacyEvent is the shape published before the envelope: Event's own fields and
// tags. It is a copy of them rather than `type legacyEvent Event` because a
// defined type inherits Event.UnmarshalJSON, and json would call it on the way
// in and recurse forever.
type legacyEvent struct {
	ID       uuid.UUID       `json:"id"`
	Name     string          `json:"name"`
	TenantID uuid.UUID       `json:"tenantId"`
	Payload  json.RawMessage `json:"payload"`
	At       time.Time       `json:"at"`
	Actor    uuid.UUID       `json:"actor"`
}

// source is the event's owner as a CloudEvents source path: the module half of
// its name, which is the first dot-separated segment. A name with no dot has the
// whole name as its module — the grammar in ValidName requires two segments, so
// only a hand-made or foreign event takes that path.
func source(name string) string {
	module, _, _ := strings.Cut(name, ".")
	return "/" + module
}

// parseUUID reads one identifier, naming the member it came from: a foreign
// producer's diagnostic has to say which attribute to fix.
func parseUUID(member, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("events: CloudEvents %s %q is not a UUID: %w", member, value, err)
	}
	return id, nil
}
