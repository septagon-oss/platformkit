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

// ServedAuthority is the address a call was answered at, as its own Host header
// named it, and "" when there was no call, no port, or a value that is not an
// address.
//
// It exists because a link this application mails has to be built from the
// address its reader can actually reach. A development installation serves every
// tenant at its name *and a port* — scripts/e2e.sh serves `localhost` on
// PLATFORMKIT_E2E_PORT, which is how a walkthrough found every mailed link
// pointing at port 80 — so a link that keeps the name and drops the port opens a
// different server, or none. Only a call knows the port it arrived on, and the
// worker that renders the mail runs long after the call is gone, which is why the
// value travels in the event that call published rather than being looked up again.
//
// The name is not for taking. A mailed link's name is its tenant's host of record,
// and a call only reaches a tenant whose record already matches the name it asked
// at (tenant.go resolves on HostOnly), so what this can contribute to a link is a
// port on a name tenancy already vouched for. A request that named no port
// contributes nothing: the scheme's default port is not a fact worth a payload
// field, and an event written by no request at all — a job, a replay — carries
// none either, which is the same absence TraceParent means.
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
	return strings.ToLower(name) + ":" + port
}
