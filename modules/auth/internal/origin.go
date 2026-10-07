package internal

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/events"
)

// Which call a mail was caused by, carried to the worker that sends it.
//
// The two functions that build a link — offer and offerVerification — are each
// reached from more than one event, and neither takes the event it came from. The
// module already solves that for one fact: the address a link is served at rides
// the event and is restored onto the handler's context by WithServed (served.go).
// The trace context and the request id are the same shape of fact and travel the
// same way, in their own slot, because a link's host and a record's provenance are
// different questions and one argument list should not answer both.
//
// They are read off the *delivered event*, not off the worker's context: a handler
// is never handed a carried kit/trace context — kit/trace.With has exactly one
// writer, kit/httpx/request_id.go, on the request path — so anything read from ctx
// here would be empty in every deployment that runs no collector, and the row
// would say nothing had asked even when something had.

type originKey struct{}

type origin struct {
	traceparent string
	requestID   string
}

// WithOrigin returns ctx carrying the trace and request id of the event a handler
// was handed, for the code that records what that handler sent. Each subscription
// whose work can end in a mail calls it, in this module's module.go and internal
// package, and nobody else has a reason to. An event with neither — a job's, a
// replay's — leaves ctx as it was and the record's two columns NULL, which is what
// migrations/000028 says an untraced row looks like.
func WithOrigin(ctx context.Context, ev events.Event) context.Context {
	if ev.TraceParent == "" && ev.RequestID == "" {
		return ctx
	}
	return context.WithValue(ctx, originKey{}, origin{ev.TraceParent, ev.RequestID})
}

// originOf is the carried trace and request id, either or both empty.
func originOf(ctx context.Context) (traceparent, requestID string) {
	o, _ := ctx.Value(originKey{}).(origin)
	return o.traceparent, o.requestID
}
