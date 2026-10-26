package httpx

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// maxAuthority bounds an authority accepted by ServedAuthority. A Host header is
// caller-supplied text; a DNS name caps at 253 characters, so anything longer is
// not an address somebody typed and is not stored in an outbox row that the audit
// trail copies.
const maxAuthority = 253

// ServedAuthority is the address a call was answered at — the name its own Host header
// named, on the port this process accepted the connection on — and "" when there was no
// call, no port, or a value that is not an address.
//
// The name comes from tenancy, which already vouched for it, and the port comes off the
// socket and never off the header, because a header is text the caller wrote. README.md,
// "The address a call was answered at" holds why a mailed link needs the port at all,
// why the socket is the truth where the two disagree, and what server.public_host answers.
func ServedAuthority(r *http.Request) string {
	if r == nil {
		return ""
	}
	name, _, ok := splitAuthority(r.Host)
	if !ok {
		return ""
	}
	served, ok := acceptedPort(r)
	if !ok {
		return ""
	}
	return name + ":" + served
}

type servedKey struct{}

// WithServed returns ctx carrying an address an earlier call was answered at, for
// the code that has to build a link out of it, and ctx unchanged for anything that
// is not an address.
//
// It exists for the worker. A subscription runs after the request that caused it is
// gone, so ServedFrom on its context would report nothing, and the link built there
// would name a port nobody serves — which is why every event that ends in a mailed
// link carries the address its own request was answered at, and the handler that
// reads such an event restores it here. The value is not a fresh claim: it is one
// this process wrote into its own outbox off a socket, and splitAuthority is the
// same parse ServedAuthority applies to a request, so a payload that spells anything
// other than a name and a port number carries nothing forward.
func WithServed(ctx context.Context, served string) context.Context {
	if _, _, ok := splitAuthority(served); !ok {
		return ctx
	}
	return context.WithValue(ctx, servedKey{}, served)
}

// ServedFrom is the address this work was answered at — the same value
// ServedAuthority reads off a request, and for the same reasons — extended to the
// work a request set in motion but outlived.
//
// A request wins when the context carries one: a request is the address, and a
// value restored from an event is a memory of one. A context with no request — a
// job, a replay, a subscription's handler — answers with what its event carried,
// or with nothing at all, which is the absence ServedAuthority already speaks: an
// installation served at the scheme's port mails no port.
//
// See WithServed for who may put a value there, and modules/auth/internal/served.go
// for what a link does with it: only the port is ever taken, and only onto a host
// tenancy already vouched for.
func ServedFrom(ctx context.Context) string {
	if r, ok := RequestFrom(ctx); ok {
		return ServedAuthority(r)
	}
	served, _ := ctx.Value(servedKey{}).(string)
	return served
}

// splitAuthority is the parse both halves of this file share: a name and a port
// number, and nothing else. net.SplitHostPort splits; it does not check that what
// follows the colon is a port number, and both a Host header and an outbox payload
// are text somebody wrote.
func splitAuthority(authority string) (name, port string, ok bool) {
	if authority == "" || len(authority) > maxAuthority {
		return "", "", false
	}
	name, port, err := net.SplitHostPort(authority)
	if err != nil || name == "" || port == "" {
		return "", "", false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", "", false
	}
	return strings.ToLower(name), port, true
}

// acceptedPort is the port this process accepted r's connection on, and false when
// the transport says nothing: a request with no connection behind it, one accepted
// on a Unix socket or a named pipe, one whose listener has no port to name.
//
// net/http puts the listener's address on every connection's context under its own
// key, which is the only address here a caller cannot write. See Kit/ClientAddr for
// the peer's half of the same socket, read the same way for the same reason.
func acceptedPort(r *http.Request) (string, bool) {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return "", false
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil || port == "" {
		return "", false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", false
	}
	return port, true
}
