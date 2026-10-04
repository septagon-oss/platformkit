// Package request is the fact that something happened *in a call*: which call,
// from where, and which distributed trace it belongs to.
//
// It exists because the outbox writes an event while the call is still open and
// the audit trail reads it back long after the call is gone. The relay publishes
// in a transaction of its own, with no request left to ask, so whatever the
// trail is going to be able to answer has to be copied onto the event row at
// the moment the event is written — which is why the actor, the trace parent and
// the trace state already live there. A request id and a client address were
// read nowhere: the request kept them to itself, and the audit row could say who
// and what and when but not which call and from where.
//
// One package, one context key, three fields, and no second copy of any of them.
// kit/trace still owns the trace format and kit/tenancy still owns the actor —
// this package carries neither, and its closure is the proof: the standard
// library and kit/trace.
//
// It does not *parse* a request. Reading net/http here would put an HTTP server
// in the closure of every worker that publishes an event, which is the boundary
// scripts/check_packages.sh guards for kit/events. So kit/httpx, the one package
// at this boundary that touches net/http, fills these values in its request-id
// middleware, and everything downstream reads them. That is also why the client
// address has exactly one parser in the tree: it used to have two
// (kit/httpx/public_writes.go and modules/auth/internal/kernel.go, written to
// the same rule independently), and the second is gone.
//
// Trace repeats what kit/trace already put on the context, and the repeat is
// deliberate: the middleware writes both keys so one reader of a call gets all
// three facts at once, and kit/trace stays the owner of the format. A context
// with one key and not the other is a middleware bug, which is why the test that
// fakes a request sets both the way the middleware does
// (modules/audit/internal/request_context_test.go).
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
