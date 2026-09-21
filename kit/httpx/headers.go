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

// Nonce is the content security policy nonce of the request ctx belongs to, or
// "" outside one. ui/page's InlineScript is the intended caller and should stay
// the only one: a template that reaches for the nonce itself is a template
// that can forget it, and the policy in htmlPolicy drops what it forgets.
func Nonce(ctx context.Context) string {
	n, _ := ctx.Value(nonceKey{}).(string)
	return n
}

// noindex is what the workspace and the control plane answer with. A crawler
// that indexes a signed-in page has read somebody's list into a search engine,
// and no amount of correct caching afterwards takes that back. The public face
// sets nothing at all: a tenant's public pages exist to be found, and this is
// the only place the difference is written.
const noindex = "noindex, nofollow"

// headers sets the three unconditional headers on the way in and the policy on
// the way out, because whether a response is a document is something only the
// response knows. A handler that set a policy of its own — the file download
// does, and it is a stricter one — keeps it.
//
// The surface decides the caching and indexing half of the policy, and it is
// known here and nowhere later: the writer below sees a status, a content type
// and a method. Everything else the three surfaces share is shared exactly as
// before this file existed.
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
		s := SurfaceOf(r.Context())
		if s != SurfacePublic {
			h.Set("X-Robots-Tag", noindex)
		}
		next.ServeHTTP(&secured{ResponseWriter: w, nonce: n, surface: s, method: r.Method},
			r.WithContext(context.WithValue(r.Context(), nonceKey{}, n)))
	})
}

// secured is the response writer that decides the policy when the status is
// written, which is the first moment the content type is known.
type secured struct {
	http.ResponseWriter
	nonce   string
	surface Surface
	method  string
	done    bool
}

func (s *secured) WriteHeader(status int) {
	s.policy(status)
	s.ResponseWriter.WriteHeader(status)
}

// Write covers the handler that writes a body without a status: net/http calls
// WriteHeader(200) itself, and by then it is too late to add a header.
func (s *secured) Write(p []byte) (int, error) {
	s.policy(http.StatusOK)
	return s.ResponseWriter.Write(p)
}

func (s *secured) policy(status int) {
	if s.done {
		return
	}
	s.done = true
	h := s.Header()
	if h.Get("Content-Security-Policy") == "" && strings.Contains(h.Get("Content-Type"), "html") {
		policy := htmlPolicy
		if s.surface != SurfacePublic {
			// The workspace may embed nothing. A document that can hold an
			// <object> can hold a form somebody else's session submits, and
			// there is no reason a page of a workspace should ever need one.
			// A handler may narrow the policy further — never widen it: the
			// stricter value is the one a handler sets beside its own response,
			// and this line is what it declines to overwrite.
			policy += "; object-src 'none'"
		}
		h.Set("Content-Security-Policy", strings.Replace(policy, "%", s.nonce, 1))
	}
	s.caching(status)
}

// caching is the difference between a face and a workspace, stated once:
//
//   - The workspace and the control plane are never cacheable, and that now
//     covers a JSON answer as much as a page. Leaving the JSON uncached was a
//     gap rather than a decision: a proxy in front of a workspace would happily
//     hold one tenant's list of users and hand it to the next person through,
//     and the response carried nothing to stop it.
//   - The public face is cacheable by default — public, one minute — on the
//     safe methods and a success, because a public page that cannot be cached
//     is a public page served from this process forever, and a 404 that a
//     cache may keep is a refusal nobody can recover from without a hard
//     reload.
//
// Either way a handler that said something about caching first is believed: it
// knows whether the bytes are somebody's own, and this function does not.
func (s *secured) caching(status int) {
	if h := s.Header(); h.Get("Cache-Control") != "" {
		return
	}
	switch {
	case s.surface != SurfacePublic:
		s.Header().Set("Cache-Control", noStore)
	case status >= 200 && status < 300 && !unsafeMethod(s.method):
		s.Header().Set("Cache-Control", publicMaxAge)
	default:
		// A refusal is never storable, on any surface. Say so rather than
		// saying nothing: an uncached-by-default 404 is still heuristically
		// cacheable by a browser, which is how a temporary condition becomes a
		// page somebody has to hard-reload to get rid of.
		s.Header().Set("Cache-Control", noStore)
	}
}

// Unwrap is how net/http's ResponseController reaches the real writer, and how
// a Flush past this one still flushes.
func (s *secured) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *secured) Flush() {
	s.policy(http.StatusOK)
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
