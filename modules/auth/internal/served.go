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
type servedKey struct{}

// maxServed bounds what a handler will read back off an event payload. A DNS name
// caps at 253 characters; an outbox row is a week old by the time it is read and
// the audit trail copies it, so anything longer is not an address somebody typed.
const maxServed = 253

// WithServed returns ctx carrying the authority askedFor was served at, for the
// handler that has to build a link from it. Each subscription that reads an
// event carrying a `served` address calls it, in this module's module.go and
// internal package, and nobody else has a reason to. An empty askedFor is the
// common case — a request that saw no port, or no request at all — and leaves ctx
// as it was.
func WithServed(ctx context.Context, askedFor string) context.Context {
	if !isAuthority(askedFor) {
		return ctx
	}
	return context.WithValue(ctx, servedKey{}, askedFor)
}

// servedPort is the port to append to a link's host, and "" when the link takes
// the scheme's default one — which is what every installation that serves its
// tenants at the scheme's port wants, and what an event written by no request
// carries.
//
// Only the port is ever taken. The name in the address the request arrived at has
// to be the host of record this link is already built on, and a host of record
// that spells its own port is left exactly as the row spells it. So the most a
// caller can put into a mailed link is the port of a request that already proved,
// by being served at all, that it belonged to that tenant.
func servedPort(ctx context.Context, host string) string {
	askedFor, _ := ctx.Value(servedKey{}).(string)
	if !isAuthority(askedFor) {
		return ""
	}
	name, port, err := net.SplitHostPort(askedFor)
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

// isAuthority is what this context may carry: a name and a port, and nothing else.
// net.SplitHostPort is the parser, so a scheme, a path, a space or a newline never
// reaches a link.
func isAuthority(s string) bool {
	if s == "" || len(s) > maxServed {
		return false
	}
	name, port, err := net.SplitHostPort(s)
	return err == nil && name != "" && port != ""
}
