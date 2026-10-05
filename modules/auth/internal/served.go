package internal

import (
	"context"
	"net"
	"strconv"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// The address a request was served at, carried to the worker that renders a
// mailed link.
//
// A link's host is the tenant's own, read from the tenant row by Delivery.Hosts:
// one customer's people must not be sent to another customer's front door. But a
// name is not the whole address. A development installation — a laptop, and every
// run of scripts/e2e.sh — serves a tenant at its name *and a port*, and a link that
// keeps the name and drops the port opens a different server, or none. Only the
// request knows the port it was answered at, and the mail is rendered in the
// worker, after the request is gone: so the port is read off the request where the
// event is published, travels in that event, and is restored onto the handler's
// context when the worker reads it back.
//
// The context carries it and not the argument list, because the two functions that
// build a link — Offer and offerVerification — are each reached from more than one
// event, and the port a link carries is nothing a caller should have to know.
//
// The slot it goes in is the kernel's, not a second one beside it. The value a
// request answers with (httpx.ServedAuthority) and the value a worker restores
// (httpx.WithServed) are one fact in one place, so a command that runs in the
// worker and publishes an event of its own — user's Invite, which raises the
// invitation this module mails — reads the address from where the request that
// started it left it, instead of reporting that nothing was served anywhere.
// See kit/httpx/authority.go.

// WithServed returns ctx carrying the authority askedFor was served at, for the
// handler that has to build a link from it. Each subscription that reads an
// event carrying a `served` address calls it, in this module's module.go and
// internal package, and nobody else has a reason to. An empty askedFor is the
// common case — a request that saw no port, or no request at all — and leaves ctx
// as it was.
func WithServed(ctx context.Context, askedFor string) context.Context {
	return httpx.WithServed(ctx, askedFor)
}

// servedPort is the port to append to a link's host, and "" when the link takes
// the scheme's default one — which is what every installation that serves its
// tenants at the scheme's port wants, and what an event written by no request
// carries.
//
// Only the port is ever taken. The name in the address the request arrived at has
// to be the host of record this link is already built on, and a host of record
// that spells its own port is left exactly as the row spells it — which is how an
// installation that declares a public port of its own wins over the socket: the
// composition carries the port server.public_host names onto this host, so an
// installation reached through a port mapping mails the port its people use and
// not the port behind the mapping, and no request is consulted for it. What this
// function can contribute is the other case: an installation that declares no
// public port and is served at a port of its own. That port is the one this
// process accepted the connection on, which httpx read off the socket where the
// event was published and carried to this line: a caller who writes a port we do
// not serve is answered at the port we do, and that is the port the link carries.
//
// Everything the address could be other than an address — a scheme, a path, a
// space, a newline, a colon with no name in front of it, a port that is not a
// number — is refused here by the same parse, and contributes nothing.
func servedPort(ctx context.Context, host string) string {
	name, port, err := net.SplitHostPort(httpx.ServedFrom(ctx))
	if err != nil || httpx.HostOnly(name) != httpx.HostOnly(host) {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "" // not a port number: nothing goes into the link
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return ""
	}
	return port
}
