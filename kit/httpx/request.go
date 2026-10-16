package httpx

// request.go carries the *http.Request into the context so a handler that needs
// the header, the host or the address the caller came from can read it.

import (
	"context"
	"net/http"
)

// carry puts the request itself on the context, because the authentication hook
// runs inside the tenant transaction — below huma's routing — and still has to
// read the caller's cookies and headers. It is two lines rather than an adapter
// unwrap so that this package does not care which adapter huma was built on.
func (a *API) carry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := withRefusalNote(r.Context())
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
