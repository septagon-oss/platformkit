package trace_test

// A caller-supplied correlation handle is bounded before it is stored.
//
// kit/httpx/request_id.go bounds the header beside this one and says why:
//
// 	// maxRequestID bounds an id a client supplied. An id is a correlation
// 	// handle, not a payload.
// 	const maxRequestID = 64
//
// 	func givenID(s string) string { … a newline or a kilobyte in it is a forged
// 	log entry or a wasted response. }
//
// tracestate is the same kind of value — a caller-supplied correlation handle
// that this program copies without reading it — and it is treated the opposite
// way: Parse keeps `state` verbatim with no bound, and kit/events/events.go
// writes exactly that string into platformkit_outbox (tracestate text,
// migration 000028) and into every event's envelope (transport.Event.TraceState
// — the attribute W3C names), and kit/events/relay.go carries it out of the row
// and back onto the wire for every subscriber, and modules/audit keeps the
// delivery. One request is therefore worth one column and one envelope
// attribute for every event that request caused: a sign-in, a task write, a
// refusal. The header that arrives beside it is bounded at 64 bytes; this one
// is bounded by net/http's own ceiling and by nothing this repository wrote.
//
// net/http caps a request's whole header block at DefaultMaxHeaderBytes
// (1 << 20), so the figure per request is a megabyte. That is a cost an
// authenticated caller can repeat, not an anonymous one: an event is written by
// a request that changes something, and an anonymous refusal writes no row.
//
// The bound asked for below is the module's share and is a decision, not a
// fact: 512 bytes is the size W3C's Trace Context specification uses when it
// discusses what a tracestate costs on the wire, and it is the size the
// OpenTelemetry specification gives the same header. Dropping an oversized
// state is allowed and is what a vendor's entry the receiver cannot keep
// means; W3C keeps a tracestate whole or drops it, so truncation mid-entry is
// the one cure that would corrupt the value, and the bound asked for below is
// written so that either answer — drop or bound — passes this case. What cannot
// pass is no rule at all. traceparent, by contrast, is already safe
// here for a reason worth stating: Context.Parent() re-renders it from parsed
// fields, so its length is a constant whatever the caller sent. That half is
// asserted too, so this case says which of the two headers needs the fix.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/trace"
)

// maxTraceState is the bound this case asks kit/trace to hold. If the module
// decides a different number, change this one line; what must not change is
// that a caller's header has a ceiling written in this repository.
const maxTraceState = 512

func TestACallersTraceStateArrivesWithinTheKernelsOwnBound(t *testing.T) {
	const parent = "00-" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "-" + "bbbbbbbbbbbbbbbb" + "-01"

	// The control, so a failure below can only be about the state: an ordinary
	// pair parses, and the parent it yields is canonical.
	short, ok := trace.Parse(parent, "ddt=qZfHpR4R3pF79i4YBiyyhq2t")
	if !ok {
		t.Fatal("an ordinary traceparent/tracestate pair did not parse")
	}
	if short.TraceState != "ddt=qZfHpR4R3pF79i4YBiyyhq2t" {
		t.Errorf("TraceState = %q, want the caller's entry kept verbatim when it fits", short.TraceState)
	}
	if got := len(short.Parent()); got != 55 {
		t.Errorf("Parent() is %d bytes, want the 55 bytes W3C fixes for version 00", got)
	}

	for _, tc := range []struct {
		name  string
		state string
	}{
		// Every value below is a value net/http can hand this package: CR and LF
		// cannot appear in a parsed header value, so nothing here claims a forged
		// log line — the claim is only the one the assertion makes, that a string
		// of whatever length a caller sends is stored and republished.
		{"a state at the ceiling of a header block", strings.Repeat("k", 1<<20)},
		{"a state of one kilobyte", strings.Repeat("vendor=entry,", 100)},
		{"a state that is what a proxy with five vendors would send", "ddt=" + strings.Repeat("x", 200) + ",scr=" + strings.Repeat("y", 200) + ",eg=" + strings.Repeat("z", 200) + ",oo=" + strings.Repeat("w", 200)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := trace.Parse(parent, tc.state)
			if !ok {
				t.Fatalf("a valid traceparent was refused because of its tracestate; a caller's oversized hint is not a reason to lose the trace")
			}
			if len(c.TraceState) > maxTraceState {
				t.Errorf("Parse kept a tracestate of %d bytes, want at most %d: the value is stored in platformkit_outbox.tracestate and republished in every event's envelope by the relay, so a caller's header is bought per event, and the sibling header this kernel writes is bounded at 64 bytes for the same reason (httpx.givenID)",
					len(c.TraceState), maxTraceState)
			}
			// The trace id survives either way: the bound on the state is not a
			// licence to lose the parent the caller sent.
			if c.Parent() != parent {
				t.Errorf("Parent() = %q, want the caller's %q", c.Parent(), parent)
			}
		})
	}
}
