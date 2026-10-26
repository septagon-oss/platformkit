// Package request is the fact that something happened *in a call*: which call,
// from where, and which distributed trace it belongs to.
//
// It exists because the outbox writes an event while the call is still open and
// the audit trail reads it back long after the call is gone: the relay publishes
// in a transaction of its own, with no request left to ask, so what the trail is
// to answer has to be copied onto the event row as the event is written. A
// request id and a client address were read nowhere before this.
//
// Request facts and operation causality use separate context slots; kit/trace
// owns the format and kit/tenancy the actor, and this package carries neither. It
// parses nothing: reading net/http here would put an HTTP server in the closure of
// every worker that publishes, so kit/httpx — the one package at this boundary
// that touches net/http — fills these values in its request-id middleware.
package request

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/trace"
)

// Context is what a call knows about itself. The zero value means "no request":
// a job, a worker, a handler reacting to another event, a test.
type Context struct {
	// ID is the request id: the one the caller was answered with in
	// X-Request-ID, and the one that appears as the instance of every refusal
	// this call produced. It is a correlation handle, not a payload — bounded
	// and printable by whoever accepted it from a client.
	ID string

	// ClientAddr is the peer address of the connection, host with no port. It
	// is never X-Forwarded-For or any other header: a header a client can
	// write is not an address, and a deployment behind a proxy that rewrites
	// RemoteAddr records the proxy, which is the truth.
	ClientAddr string

	// Trace is the W3C trace context of the call: kit/trace parsed it from the
	// caller's traceparent, opened it from the request id, or minted it for a
	// request whose id could not be a trace id. Invalid is the normal case for
	// work nobody requested — a job, a relayed row — and not for a call.
	Trace trace.Context
}

// key is the one slot. Unexported, so nothing outside this package can fill it
// and a value that arrives from somewhere else is a bug rather than a forgery.
type key struct{}

// With returns ctx carrying r. kit/httpx's request-id middleware is its only
// caller; anything else that wants to claim a request must be a request.
func With(ctx context.Context, r Context) context.Context {
	return context.WithValue(ctx, key{}, r)
}

// From returns the call ctx belongs to and whether there was one. A publisher
// that gets false writes NULL — an absent request is a fact, not a gap.
func From(ctx context.Context) (Context, bool) {
	r, ok := ctx.Value(key{}).(Context)
	return r, ok
}
