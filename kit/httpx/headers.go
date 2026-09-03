package httpx

// headers.go is the response every request gets whatever the handler did: the
// three headers a browser needs before it decides what a document may do, and
// the content security policy for the ones that are documents.
//
// They are set here rather than by a reverse proxy because a reference
// architecture that relied on one would be teaching a deployment topology, and
// because a header a proxy adds is a header a request that reaches the pod
// directly does not have. New puts this outermost, on the router that carries
// the static tree as well as the API, so a stylesheet and a 500 are both
// covered.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
)

// The three headers every response carries. Framing and referrers are decided
// per response and not per document, so they belong here rather than in a
// <meta>: a JSON error page has no head to put one in.
//
// nosniff is set here as well as by the file download, which sets it beside a
// Content-Disposition it has its own reason for: one of the two will outlive
// the other, and neither is a good place to be the only one.
const (
	frameOptions   = "DENY"
	referrerPolicy = "strict-origin-when-cross-origin"
	noSniff        = "nosniff"
)

// htmlPolicy is the policy a document is served under. Everything comes from
// this origin; nothing frames it; images may also be data: URLs, which is what
// an inline SVG icon and a generated chart are.
//
// script-src carries the request's nonce rather than 'unsafe-inline', so the
// one inline script in the application — modules/admin's theme snippet, which
// has to run before the first paint or the page flashes white — runs and
// anything a body smuggled in does not.
//
// style-src carries 'unsafe-inline', and it is the one concession: ui/components
// emits style attributes for a table column's width and for an element that is
// hidden (ui/components/molecules.go), and a nonce cannot cover a style
// attribute — CSP nonces apply to elements, and 'unsafe-hashes' would mean
// listing every width anybody ever writes. The exposure is CSS, not script.
//
// base-uri and form-action are the two the review found missing, and they are
// the two that make the rest hold. Without base-uri an injected <base> retargets
// every relative URL on the page, so 'self' stops meaning this origin; without
// form-action an injected form posts what a person typed to somebody else, and
// no source list covers where a form goes.
const htmlPolicy = "default-src 'self'; script-src 'self' 'nonce-%'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// hsts is a year, subdomains included. It is set on every response of a
// deployment that is not reached at a local name, and on none of a deployment
// that is: a browser told to use https for localhost is a laptop that cannot
// reach its own application until somebody clears the header, and there is no
// way to say "except this one" once it is cached.
//
// No preload directive. Preloading is a submission to a list browsers ship, and
// it is not this application's to make on a deployment's behalf — a deployment
// that wants it adds the directive at its edge and means it.
const hsts = "max-age=31536000; includeSubDomains"

// noStore is what an authenticated page and a private download carry.
//
// A browser caches a document nobody told it not to, and so does every proxy
// between here and it: a tenant's admin page or a private file left in a shared
// cache is that tenant's data served to whoever asks next. It is set on the
// responses that carry somebody's own data and not on the anonymous ones,
// because a public page that could not be cached is a public page served from
// this process forever.
const noStore = "no-store"

type nonceKey struct{}

// NonceFrom is the content security policy nonce of the request ctx belongs to,
// or "" outside one. A template that has to emit an inline <script> puts it on
// the tag; nothing else needs it.
func NonceFrom(ctx context.Context) string {
	n, _ := ctx.Value(nonceKey{}).(string)
	return n
}

// headers sets the three unconditional headers on the way in and the policy on
// the way out, because whether a response is a document is something only the
// response knows. A handler that set a policy of its own — the file download
// does, and it is a stricter one — keeps it.
func (a *API) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", frameOptions)
		h.Set("Referrer-Policy", referrerPolicy)
		h.Set("X-Content-Type-Options", noSniff)
		if !config.Local(a.opts.PublicHost) {
			h.Set("Strict-Transport-Security", hsts)
		}
		n := nonce()
		// Whether the caller presented a credential is what decides no-store,
		// and it is known here and nowhere later: the writer below sees a
		// content type and no request.
		next.ServeHTTP(&secured{ResponseWriter: w, nonce: n, private: credentialed(r)},
			r.WithContext(context.WithValue(r.Context(), nonceKey{}, n)))
	})
}

// secured is the response writer that decides the policy when the status is
// written, which is the first moment the content type is known.
type secured struct {
	http.ResponseWriter
	nonce string
	// private is a request that presented a credential, so its HTML is
	// somebody's own and not a page a cache may keep.
	private bool
	done    bool
}

func (s *secured) WriteHeader(status int) {
	s.policy()
	s.ResponseWriter.WriteHeader(status)
}

// Write covers the handler that writes a body without a status: net/http calls
// WriteHeader(200) itself, and by then it is too late to add a header.
func (s *secured) Write(p []byte) (int, error) {
	s.policy()
	return s.ResponseWriter.Write(p)
}

func (s *secured) policy() {
	if s.done {
		return
	}
	s.done = true
	h := s.Header()
	if h.Get("Content-Security-Policy") != "" {
		return // the handler has a stricter one; see modules/file
	}
	if strings.Contains(h.Get("Content-Type"), "html") {
		h.Set("Content-Security-Policy", strings.Replace(htmlPolicy, "%", s.nonce, 1))
		// An authenticated document is one tenant's own. A handler that has
		// already said something about caching keeps it.
		if s.private && h.Get("Cache-Control") == "" {
			h.Set("Cache-Control", noStore)
		}
	}
}

// Unwrap is how net/http's ResponseController reaches the real writer, and how
// a Flush past this one still flushes.
func (s *secured) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *secured) Flush() {
	s.policy()
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// nonce is 128 bits, base64, per request. crypto/rand.Read cannot fail on any
// platform this runs on — it panics rather than returning an error — so there
// is nothing here to handle.
func nonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawStdEncoding.EncodeToString(b[:])
}
