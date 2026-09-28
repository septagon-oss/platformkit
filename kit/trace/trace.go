// Package trace carries the W3C Distributed Tracing context of a request
// through the process that handled it and into the events it caused.
//
// It is a carrier, not a tracer: there is no span collection, no exporter and
// no sampling decision here, because the metrics pillar (decision 0052 §3 item 6)
// owns those and has not landed. What this package fixes is the format and the
// one question an event can answer — which request caused me — so that a
// collector added later has something to join to rather than a format to
// retrofit. The id is the same identifier the log line carries as request_id and
// the problem body carries as its instance, which is what makes the join real
// today: an event's traceparent names the request that caused it in the terms
// every log of that request already uses.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Header names, written once. A request that arrives with a traceparent is
// part of a trace somebody else started, and the one place that reads it and
// the one place that would write it have to spell the header the same way.
const (
	ParentHeader = "traceparent"
	StateHeader  = "tracestate"
)

// version is the only W3C traceparent version this code writes. A version a
// caller sent that this package cannot parse is treated as no context at all,
// which is what the specification's "ignore what you cannot understand" asks
// for; version 255 (ff) is reserved and never accepted.
const version = "00"

// flagsSampled is the flag byte written on a context this process minted. The
// decision it records is "a collector that arrives later will want this trace",
// which is the only honest answer available before there is a sampler.
const flagsSampled = "01"

// Context is one distributed tracing context: which trace, which span in it,
// and whatever vendor state came in the header.
type Context struct {
	// TraceID is 32 lower-case hex characters.
	TraceID string
	// SpanID is 16 lower-case hex characters.
	SpanID string
	// TraceState is the unchanged tracestate header, kept verbatim because a
	// vendor's entry in it is not this program's to interpret or reorder.
	TraceState string
}

// Parent renders the traceparent header value, or "" when there is no context.
// Every writer of the attribute — the envelope, the outbox column, the header
// itself — goes through here, so the format is one function's output.
func (c Context) Parent() string {
	if !c.Valid() {
		return ""
	}
	return version + "-" + c.TraceID + "-" + c.SpanID + "-" + flagsSampled
}

// Valid reports whether both ids are present in the form W3C fixes: lower-case
// hex, exact width, and not all zero, which the specification reserves.
func (c Context) Valid() bool {
	return hexID(c.TraceID, 32) && hexID(c.SpanID, 16)
}

func hexID(s string, width int) bool {
	if len(s) != width || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	// All-zero is a valid hex string and an invalid identifier.
	return err == nil && s != strings.Repeat("0", width)
}

// Parse reads a traceparent and tracestate header pair. A header that is
// malformed, of a version this package cannot read, or that carries an id the
// specification reserves is no context at all: a caller's broken header is not
// a reason to corrupt a trace, and it is not a reason to refuse a request
// either, which is why this returns a bool rather than an error.
func Parse(parent, state string) (Context, bool) {
	fields := strings.Split(parent, "-")
	if len(fields) < 4 || fields[0] != version {
		return Context{}, false
	}
	c := Context{TraceID: fields[1], SpanID: fields[2], TraceState: state}
	if !c.Valid() {
		return Context{}, false
	}
	return c, true
}

// New mints a context of its own: a fresh trace id and a fresh span id.
func New() Context {
	return Context{TraceID: randHex(16), SpanID: randHex(8)}
}

// FromRequestID starts a trace whose id is the request's own identifier, when
// that identifier is a UUID-shaped hex string. The join is the point: the
// request id a caller quoted, the log line's request_id and the event's
// traceparent are then one identifier rather than three that happen to agree.
// An id that is not 32 hex characters — a proxy's opaque handle, within the
// length httpx accepts — cannot be a W3C trace id, and this says so rather than
// hashing something into a shape it does not have.
func FromRequestID(id string) (Context, bool) {
	id = strings.ReplaceAll(id, "-", "")
	if !hexID(id, 32) {
		return Context{}, false
	}
	return Context{TraceID: id, SpanID: randHex(8)}, true
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand has no failure to return in practice; if it ever does,
		// an event with no trace parent is the answer, not a panic.
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

type key struct{}

// With returns ctx carrying c, for the code downstream of the request that
// received it — including the outbox write of every event that request caused.
func With(ctx context.Context, c Context) context.Context {
	if !c.Valid() {
		return ctx
	}
	return context.WithValue(ctx, key{}, c)
}

// From returns the context ctx carries, if any. A periodic job, a worker that
// relayed a row, and a test that called a service directly all land here.
func From(ctx context.Context) (Context, bool) {
	c, ok := ctx.Value(key{}).(Context)
	return c, ok
}
