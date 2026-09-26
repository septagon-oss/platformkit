package httpx

// csrf.go refuses a cross-site write. An unsafe method from an origin that is
// not this host is refused before it reaches a handler.

import (
	"net/http"
	"net/url"
)

// unsafeMethod reports whether a method may change state. The four safe ones are
// the closed set RFC 9110 calls safe; everything else is guarded.
func unsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}

// csrf refuses a state-changing request that a cookie session authorised and
// another site sent.
//
// A cookie is attached by the browser whichever page made the request, so a
// session cookie is a credential the caller did not choose to present. The check
// is the browser's own account of where the request came from: Sec-Fetch-Site,
// which a page cannot forge, and an Origin whose host is this one for the older
// clients that do not send it. A request carrying no session cookie is not
// guarded, because nothing was attached on its behalf — a bearer token is
// presented deliberately and a cross-site page cannot read one to present.
//
// # What this accepts
//
// One case is let through that a per-form token would not, and it is named here
// rather than implied: a state-changing request with a session cookie, no
// Sec-Fetch-Site and no Origin. No browser in support produces it — every
// current engine sends the first, and the ones before it sent the second on a
// cross-origin POST, and a page that could suppress both could suppress a token
// header too. What does produce it is a script or a mobile client, which
// attached the cookie because somebody wrote the code that attaches it. That is
// the deliberate presentation this check exists to tell from the automatic one.
// If a client that suppresses both headers and can be driven cross-site ever
// exists, this is the line that changes, and it is one line.
func (a *API) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SessionCookieOf(r); !ok || !unsafeMethod(r.Method) || SameSite(r) {
			next.ServeHTTP(w, r)
			return
		}
		a.rlog(r.Context()).InfoContext(r.Context(), "httpx: cross-site write refused",
			"method", r.Method, "path", r.URL.Path, "site", r.Header.Get("Sec-Fetch-Site"), "origin", r.Header.Get("Origin"))
		a.fail(w, r, http.StatusForbidden,
			CodeCSRFOrigin+": this request carries a session cookie and came from another site")
	})
}

// SameSite is the browser's own account of where a request came from:
// Sec-Fetch-Site, which a page cannot forge, and an Origin whose host is this
// one for the older clients that do not send it. A request that says neither is
// not a browser, and reads as same-site — see the middleware above for why that
// is the deliberate answer and not a hole.
//
// It is exported for the one route the middleware cannot cover: a sign-in
// carries no session cookie, so nothing was attached on the caller's behalf and
// the middleware lets it through. That is right for the general rule and wrong
// for that route, because a cross-site sign-in mints a credential rather than
// spending one — the attacker signs the visitor into the attacker's own account
// and then reads what the visitor does in it. modules/auth asks this directly.
func SameSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// No Sec-Fetch-Site at all: an older client, or a non-browser. The
		// Origin header is the fallback, and its absence is accepted for the
		// same reason — a caller that is not a browser presents what it presents
		// deliberately.
		origin := r.Header.Get("Origin")
		return origin == "" || sameHost(origin, r.Host)
	}
	return false
}

// sameHost reports whether origin names this request's own host.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && HostOnly(u.Host) == HostOnly(host)
}
