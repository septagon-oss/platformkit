package httpx

// cookies.go names the session cookie. The name depends on the scheme, and the
// names an earlier release used are still read so a signed-in person stays so.

import (
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/appname"
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

// CookieName returns base with the __Host- prefix when secure is true.
//
// Deprecated: Use CookieNameFor with the zero appname.Name.
func CookieName(base string, secure bool) string { return CookieNameFor("", base, secure) }

// CookieNameOf returns the cookie name under app, adding __Host- when secure.
//
// Deprecated: Use CookieNameFor with the same app, base and secure values.
func CookieNameOf(app appname.Name, base string, secure bool) string {
	return CookieNameFor(app, base, secure)
}

// CookieNameFor returns appname.Cookie's spelling of app and base, without
// additional validation. The zero app preserves the unscoped name. secure adds
// __Host-; the caller must also set Secure, Path=/ and no Domain so sibling hosts
// cannot set a cookie the browser sends here. base is forwarded unchanged.
func CookieNameFor(app appname.Name, base string, secure bool) string {
	return appname.Cookie(app, base, secure)
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
	if r.Header.Get("Cookie") == "" {
		// Nothing to look for, and the search is not free: every spelling of the
		// session cookie's name is built before the first read, so an anonymous
		// request paid for a list of names it cannot carry. The Cookie header is
		// the only place a request presents a cookie — net/http reads no other —
		// which is what makes this the same answer, reached without the list.
		return nil, false
	}
	for _, name := range append([]string{CookieNameFor("", SessionCookie, true), SessionCookie}, legacyNames()...) {
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
		out = append(out, CookieNameFor("", base, true), base)
	}
	return out
}

// BearerOf returns the bearer credential a request presented, if it presented
// one. The scheme is matched case-insensitively, as RFC 7235 requires of a
// scheme, and the credential is returned verbatim.
//
// A request that names the scheme twice, or twice with different values, is
// refused rather than resolved: which of two credentials the caller meant is not
// something the kernel can guess, and the safe answer to an ambiguous credential
// is that none was presented. A header with the scheme and nothing after it is the
// same refusal.
func BearerOf(r *http.Request) (string, bool) {
	const scheme = "bearer"
	var found string
	for _, line := range r.Header.Values("Authorization") {
		kind, rest, ok := strings.Cut(line, " ")
		if !ok || !strings.EqualFold(kind, scheme) {
			continue
		}
		credential := strings.TrimSpace(rest)
		if credential == "" || (found != "" && found != credential) {
			return "", false
		}
		found = credential
	}
	return found, found != ""
}
