package httpx

// request.go carries the *http.Request into the context so a handler that needs
// the header, the host or the address the caller came from can read it.

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// carry puts the request itself on the context, because the authentication hook
// runs inside the tenant transaction — below huma's routing — and still has to
// read the caller's cookies and headers. It is two lines rather than an adapter
// unwrap so that this package does not care which adapter huma was built on.
func (a *API) carry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := withRefusalNote(r.Context())
		if a.opts.Cache != nil {
			ctx = context.WithValue(ctx, cacheKey{}, a.opts.Cache)
		}
		if a.access != nil {
			ctx = withAccessDoor(ctx, a.access)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestKey{}, r)))
	})
}

// requestKey carries the *http.Request past huma's routing. See carry.
type requestKey struct{}

// RequestFrom is the request being served.
//
// A handler wants it for the things huma's typed input cannot express and that
// are properties of the connection rather than of the operation: the host an
// absolute redirect has to be built for, and the address and user agent a
// session records so that a person can recognise it in a list. Reading the body
// through it is a mistake — huma has already decoded it — and reading the
// context off it is another, because this is the request as it was before the
// tenant, the transaction and the principal were put on it.
func RequestFrom(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// RetryAfter answers "when, then" on the response this request is being given, for a refusal
// whose remedy is to ask again rather than to ask differently.
//
// The header is the answer's and not the error's: kit/problem carries the body and nothing
// else, and a caller that retries blind either hammers a saturated machine or waits a minute
// for a slot that frees in five seconds. So the hint is written here, on the response
// kit/httpx is already holding — the one object both a handler and the guards in this chain
// can reach, and the writer that decides when the headers go out.
//
// It returns whether there was still a response to write to: a route that has begun its
// stream has sent its headers, and a call outside a request has no response at all. False
// costs the caller the hint and never the refusal, and the seconds never round to zero.
func RetryAfter(ctx context.Context, wait time.Duration) bool {
	b, ok := bufferFrom(ctx)
	if !ok || b.begun() {
		return false
	}
	b.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()))))
	return true
}
