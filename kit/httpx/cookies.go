package httpx

// cookies.go names the session cookie. The name depends on the scheme, and the
// names an earlier release used are still read so a signed-in person stays so.

import (
	"net/http"
)

// SessionCookie is the base name of the cookie a browser session travels in.
// It is here, and not in the auth module that mints it, because the kernel has
// to recognise it twice without knowing anything else about sessions: to refuse
// a cross-site write (csrf) and to decide that a request is worth
// authenticating at all.
//
// It names the workspace, not the product: the cookie is only ever set on the
// App surface, and a name that says "session" rather than "platformkit_session"
// is one a person reading their own cookie list can make sense of. See
// SessionCookieOf for what is still read.
const SessionCookie = "session"

// legacySessionCookies are the names this release still reads and never writes.
// A browser with a jar full of the old name is a person who signed in before the
// upgrade, and refusing them would be an outage nobody announced; sign-in and
// sign-out both clear these, so a jar carries the new name alone from the first
// visit after the upgrade. Removed with the aliases, in v1.3.0.
var legacySessionCookies = []string{"platformkit_session"}

// CookieName is the name a first-party cookie is set under, which depends on
// whether it will carry Secure.
//
// __Host- is a rule the browser enforces: a cookie named with it is accepted
// only with Secure, Path=/ and no Domain, and a page on another host cannot set
// one the browser will send here. That last part is why it matters here — every
// tenant is reached at its own host, often as siblings under one registrable
// domain, so without the prefix a page at one customer's host can set
// platformkit_session for the parent domain and have it attached at every
// other customer's. It is dropped when the cookie is not Secure, because a
// browser refuses one of those over http://localhost; both names are recognised
// on the way in, and a deployment only ever presents one.
func CookieName(base string, secure bool) string {
	if secure {
		return "__Host-" + base
	}
	return base
}

// SessionCookieOf is the session cookie the request presents, under either
// name. It is exported because the auth module reads the one the kernel
// recognised: two spellings of "which cookie is the session" is a session one
// half of the program can see and the other cannot.
//
// The order is the answer: the current name first, so a jar that carries both —
// which is what a jar upgraded without clearing looks like — spends the session
// the current code minted rather than the older one. The legacy names are read
// for one release and written never.
func SessionCookieOf(r *http.Request) (*http.Cookie, bool) {
	for _, name := range append([]string{CookieName(SessionCookie, true), SessionCookie}, legacyNames()...) {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c, true
		}
	}
	return nil, false
}

// legacyNames is every legacy spelling, __Host- prefixed and plain, of the
// cookie names this release still reads. It is exported through
// LegacySessionCookies because the auth module has to *clear* what the kernel
// still reads, and two lists of the same names drift.
func LegacySessionCookies() []string { return legacyNames() }

func legacyNames() []string {
	var out []string
	for _, base := range legacySessionCookies {
		out = append(out, CookieName(base, true), base)
	}
	return out
}
