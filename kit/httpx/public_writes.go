package httpx

// public_writes.go bounds what an anonymous visitor may submit, per tenant, per
// route and per address, so two customers never share a counter.

import (
	"context"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// publicWrites bounds what an anonymous visitor may ask for — the Public surface's only rate
// limit, because that surface has no account to lock out: an anonymous form post has no
// identity beyond the address it came from. The key names the tenant the request's own host
// resolved to, the route asked for and the address it was asked from, so two customers never
// share a counter and one office does not spend another's minute.
//
// It runs after the tenant middleware, whose host lookup needs no request transaction, and
// ahead of the request's own, so a refused write is not a transaction that rolled back an
// attempt nobody made and the count survives the refusal on a detached context. Safe methods
// are never counted, and a limiter that cannot answer fails open and logs: the honest failure
// mode of a limit is the traffic it lets through, not the refusals it invents. An attempt the
// counter did not record arrives as ok false with no error at all and is refused like any
// other spent window (kit/limit/README.md owns the four outcomes), through refuse, because
// this caller is at a form in a browser.
func (a *API) publicWrites(ctx huma.Context, next func(huma.Context)) {
	if a.opts.WriteLimiter == nil || SurfaceOf(ctx.Context()) != SurfacePublic || !unsafeMethod(ctx.Method()) {
		next(ctx)
		return
	}
	r, ok := RequestFrom(ctx.Context())
	if !ok {
		next(ctx)
		return
	}
	// The route is the mounted pattern and not the request's own path, which is
	// the difference between a bounded set of counters and one per slug anybody
	// types into a public address.
	t, _ := tenancy.FromContext(ctx.Context())
	key := publicWriteKey + " " + t.Slug + " " + ctx.Method() + " " + routeOf(ctx) + " " + ClientAddr(r)
	allowed, remaining, retryAfter, err := a.opts.WriteLimiter.Reserve(context.WithoutCancel(ctx.Context()), key, publicWriteLimit, publicWriteWindow)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the public write limit could not be read; proceeding",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		next(ctx)
		return
	}
	// Both fields go on every answer this limit applies to and not only the refusal:
	// the whole point of stating a limit on the wire is that a client backs off before
	// it runs out rather than after, and a client that only hears the number when it
	// is refused has already spent the traffic the header exists to save.
	//
	// The policy line costs no statement — it is the name, quota and window this guard
	// was built with. The rate line is what the counter itself reported, and it is the
	// same reading that decided this answer: Reserve records the event and returns the
	// quota left and the window left from that one statement, so the number a caller
	// is given is the number that was true of their own attempt rather than one taken
	// from the row after another request had moved it. That is the whole of why this
	// port has one method: a `Count` after the decision is a second look at a row that
	// has had other writers since, and a limit stated from it is a guess.
	sayPolicy(ctx, publicWritePolicy)
	sayAnswer(ctx, measured{policy: publicWritePolicy, remaining: remaining, left: retryAfter})
	if allowed {
		next(ctx)
		return
	}
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: public write refused by the limit",
		"method", ctx.Method(), "path", ctx.URL().Path, "tenant", t.Slug)
	sayRetryAfter(ctx, retryAfter)
	a.refuse(ctx, http.StatusTooManyRequests, CodeLimitExhausted+": too many anonymous submissions from this address")
}

// routeOf is the mounted pattern the request matched, which is the route half of
// the public write key. The request's own path is not it: a public address with
// a slug in it would get one counter per slug anybody typed, and a counter that
// anyone can inflate is a table anyone can fill.
func routeOf(ctx huma.Context) string {
	if op := ctx.Operation(); op != nil && op.Path != "" {
		return op.Path
	}
	return ctx.URL().Path
}

// ClientAddr is the peer address of the request, which is the only address here
// that a caller cannot write: no X-Forwarded-For, for the reason the auth module
// gives for the same rule. A deployment behind a proxy that rewrites RemoteAddr
// gets the right one anyway.
//
// It is exported, and it is the only parser of an address in the tree, because
// two were written to that rule independently (this one and
// modules/auth/internal/kernel.go's) and a rule with two implementations is a
// rule one of them will break. kit/request cannot own it: parsing net/http here
// is what keeps net/http out of the closure of every worker that publishes.
func ClientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
