// Package trace carries the W3C Distributed Tracing context of a request
// through the process that handled it and into the events it caused.
//
// It is a carrier, not a tracer: there is no span collection, no exporter and
// no sampling decision here, because kit/telemetry names that vocabulary and
// kit/app installs the one provider (decision 0052 §3 item 6). What this package
// fixes is the format and the one question an event can answer — which request
// caused me — so the outbox row and the envelope carry a W3C context in a process
// that installed no provider at all, and a collector has something to join to.
// The id is the same identifier the log line carries as request_id and
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

// MaxTraceState bounds the vendor state a caller may hand this program. A
// tracestate is a correlation handle, not a payload: kit/events stores exactly
// this string in platformkit_outbox.tracestate and the relay republishes it on
// the envelope of every event the request caused, so an unbounded header is
// paid for once per event, and a header this package would see at all is
// bounded only by net/http's own 1 MiB header-block ceiling. The sibling this
// kernel writes bounds its caller-supplied id at 64 bytes for the same reason
// (httpx.givenID). 512 is the figure the W3C Trace Context and OpenTelemetry
// specifications both use when they price this header on the wire.
const MaxTraceState = 512

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
	// TraceState is the tracestate header, kept verbatim within MaxTraceState:
	// a vendor's entry in it is not this program's to interpret or reorder, so
	// what is kept is kept whole and what cannot fit is dropped, not rewritten.
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
	// The caller's hint is trimmed, never cut mid-entry, and it is trimmed here
	// rather than at each writer, so the row, the envelope and the header can
	// only ever carry one and the same bounded value.
	c := Context{TraceID: fields[1], SpanID: fields[2], TraceState: withinStateBound(state)}
	if !c.Valid() {
		return Context{}, false
	}
	return c, true
}

// withinStateBound trims a tracestate to MaxTraceState at entry boundaries.
// W3C keeps an entry whole or drops it, so the one cure that would corrupt the
// value — slicing it mid-vendor — is not what happens here: the entries that
// fit are kept in the order the caller sent them and the first one that does
// not fit ends the header. An entry too large on its own therefore drops the
// state and keeps the trace, which is the pair worth having: the caller's
// opaque hint is the cheaper half of a parsed context to lose.
func withinStateBound(state string) string {
	if len(state) <= MaxTraceState {
		return state
	}
	var kept []string
	size := 0
	for _, entry := range strings.Split(state, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		width := len(entry)
		if len(kept) > 0 {
			width += 2 // the ", " this package writes back out between entries
		}
		if size+width > MaxTraceState {
			break
		}
		kept, size = append(kept, entry), size+width
	}
	return strings.Join(kept, ", ")
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
