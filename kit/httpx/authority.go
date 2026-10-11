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

// ServedAuthority is the address a call was answered at — the name its own Host
// header named, on the port this process accepted the connection on — and "" when
// there was no call, no port, or a value that is not an address.
//
// It exists because a link this application mails has to be built from the address
// its reader can actually reach. A development installation serves every tenant at
// its name *and a port* — scripts/e2e.sh serves `localhost` on the port it decided,
// which is how a walkthrough found every mailed link pointing at port 80 — so a
// link that keeps the name and drops the port opens a different server, or none.
// Only a call knows the port it arrived on, and the worker that renders the mail
// runs long after the call is gone, which is why the value travels in the event
// that call published rather than being looked up again.
//
// The name is not for taking. A mailed link's name is its tenant's host of record,
// and a call only reaches a tenant whose record already matches the name it asked
// at (tenant.go resolves on HostOnly), so what this can contribute to a link is a
// port on a name tenancy already vouched for.
//
// The port is not for taking either, which is why it is read off the socket and not
// off the header. Host is text the caller wrote: one request that reached this
// listener, aimed at `acme.localhost:7`, used to be enough to make a victim's reset
// mail lead to port 7, where nothing of ours listens — a link carrying a live
// credential, addressed by the person best placed to phish for it. The port a
// connection arrived on is the peer of that listener and cannot be chosen from
// inside a request, so it is what the link says. Where the two disagree, the socket
// is the truth and the header was a claim: the call was answered at the port the
// answer names.
//
// A request that named no port contributes nothing even though its socket has one:
// naming no port is how a browser says it reached us at the scheme's default, which
// is the case for every installation fronted by a proxy that terminates on 443 and
// forwards to a port of its own — whose internal port must not become a public
// address. An installation whose *public* port is neither the scheme's default nor
// the one its socket listens on — a container published with -p 38591:8080, a
// compose ports: entry, a NodePort — cannot be answered by either half of this: the
// header's port is a claim and the socket's is the private one. Such an installation
// declares the public port in server.public_host, and the application writes that
// port onto the host of record it hands its link builders, which is then the one
// spelling a mailed link is built from without consulting any request, and one this
// function leaves alone (see servedPort in modules/auth/internal/served.go: a host
// that already spells a port is never rewritten by the socket). What is left here is
// the port of an installation that declares no public one. A request with no socket
// at all — a job, a replay, a handler driven in-process — is served nowhere, and
// carries nothing, the same absence TraceParent means.
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
