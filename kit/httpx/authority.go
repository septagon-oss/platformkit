package httpx

import (
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
// address. An installation whose *public* port is a non-default one declares it in
// its host record, which is the one spelling a mailed link is built from without
// consulting any request (see servedPort in modules/auth/internal/served.go). A
// request with no socket at all — a job, a replay, a handler driven in-process — is
// served nowhere, and carries nothing, the same absence TraceParent means.
func ServedAuthority(r *http.Request) string {
	if r == nil || len(r.Host) > maxAuthority {
		return ""
	}
	name, port, err := net.SplitHostPort(r.Host)
	if err != nil || name == "" || port == "" {
		return ""
	}
	// net.SplitHostPort splits; it does not check that what follows the colon is a
	// port number, and a Host header is text somebody typed.
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ""
	}
	served, ok := acceptedPort(r)
	if !ok {
		return ""
	}
	return strings.ToLower(name) + ":" + served
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
