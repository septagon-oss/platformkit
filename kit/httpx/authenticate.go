package httpx

// authenticate.go recognises the caller, and opens the connection the rest of the
// request path resolves its tenant and its grants on.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// authenticate establishes the caller's principal, inside the tenant
// transaction the request has just been given.
//
// It asks the hook only when the request carries something to recognise. That
// is not an optimisation: obtaining the transaction opens it, and a request that
// opens a transaction is a request that fails while the database is down — which
// is exactly what the lazy transaction exists to avoid for a liveness probe
// addressed to a tenant host.
//
// A hook that reports false leaves the context anonymous, which is not an error
// here: only the authorization middleware knows whether the operation minds. A
// hook that returns an error is an outage and answers 500, because a session
// store that cannot be read must not read as "you are not signed in".
func (a *API) authenticate(ctx huma.Context, next func(huma.Context)) {
	// The public surface reads no session at all. A visitor's request that
	// happens to carry a cookie is answered as the anonymous request it is —
	// which is not an optimisation but the surface's promise, and the reason a
	// public route can never be the one that opens a transaction to be told it
	// is anonymous. The mount gate (R1) refuses any public route that would need
	// a principal, so nothing here is left unsatisfiable by the skip.
	if SurfaceOf(ctx.Context()) == SurfacePublic {
		next(ctx)
		return
	}
	r, ok := RequestFrom(ctx.Context())
	if !ok || !credentialed(r) {
		next(ctx)
		return
	}
	if ambiguousCredential(r) {
		// Both credentials at once is not a caller who is more signed in. Which
		// one the caller meant is not the kernel's to guess, and the safe answer
		// to an ambiguous credential is none: the request goes on as anonymous,
		// which means it is refused by whatever guard the operation declares
		// rather than by a coin toss here. Logged, because a client doing this is
		// a client with a bug worth finding.
		a.rlog(ctx.Context()).WarnContext(ctx.Context(),
			"httpx: a request presented both a session cookie and a bearer token; answering as anonymous")
		next(ctx)
		return
	}
	tx, ok := TxFrom(ctx.Context())
	if !ok {
		// No tenant, or no transaction. Either way there is nowhere to look the
		// caller up, and the transaction middleware has already logged the cause.
		next(ctx)
		return
	}
	p, ok, err := a.opts.Authenticate(ctx.Context(), tx, r)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not recognise the caller",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuse(ctx, http.StatusInternalServerError, "")
		return
	}
	if !ok {
		next(ctx)
		return
	}
	// One recognition, two things put on the context: the principal, which a
	// handler and the authorization middleware ask about, and the actor, which
	// kit/events stamps on every event this request publishes. Deriving the
	// second here rather than at each Publish is what keeps "who did this" out
	// of every module's argument lists.
	next(huma.WithContext(ctx, tenancy.WithActor(tenancy.WithPrincipal(ctx.Context(), p), p.UserID)))
}

// credentialed reports whether the request presents something the application
// could recognise: the session cookie, or a bearer token.
//
// The Authorization header used to be accepted here and did nothing, which was a
// transaction for every request carrying one and a recognition for none — the
// hook had no bearer to look up, so the only answer was "not signed in". It is
// accepted now because modules/auth mints bearer tokens and can name the person
// one belongs to. No cookie is ever set, rotated or cleared by a request that
// arrived on a bearer: the token is the caller's own proof of intent, which is
// why csrf.go needs no change for it either — its gate is the session cookie.
func credentialed(r *http.Request) bool {
	_, cookie := SessionCookieOf(r)
	_, bearer := BearerOf(r)
	return cookie || bearer
}

// ambiguousCredential reports whether one request carries two different kinds of
// credential at once. Two of the same kind is not ambiguity: a cookie replayed by
// an old proxy and the cookie the app just set are the same thing.
func ambiguousCredential(r *http.Request) bool {
	_, cookie := SessionCookieOf(r)
	_, bearer := BearerOf(r)
	return cookie && bearer
}

// ConnFrom is the application connection this request is served on.
//
// It is the second half of SystemToken: db.RunSystem takes a connection and a
// capability, and a module holds neither until it is handed them. A module that
// has not taken a token can do nothing with this that it could not already do
// with the request's own transaction.
func ConnFrom(ctx context.Context) (*db.Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*db.Conn)
	return c, ok
}

// WithConn puts a connection on ctx. The transaction middleware calls it for
// every request; it is exported so that a test can put a service in the same
// position a request puts it in, rather than the service growing a second code
// path that exists to be testable.
func WithConn(ctx context.Context, c *db.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}

type connKey struct{}
