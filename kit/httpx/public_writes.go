package httpx

// public_writes.go bounds what an anonymous visitor may submit, per tenant, per
// route and per address, so two customers never share a counter.

import (
	"context"
	"net"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// publicWrites bounds what an anonymous visitor may ask for.
//
// It is the Public surface's only rate limit, and it exists because that surface
// is the one with no account to lock out: the auth module counts sign-ins, and
// an anonymous form post has no identity to count against beyond the address it
// came from. So the key names three things — the tenant the request's own host
// resolved to, the route the caller asked for, and the address they asked from —
// and two customers therefore never share a counter, one customer's office does
// not exhaust another's, and a machine hammering the sign-up form does not spend
// the same minute as the password reminders beside it.
//
// Three things about where it runs, all load-bearing:
//
//   - After the tenant middleware, and ahead of the request's transaction. The
//     first half is what puts a tenant in the key: the host lookup runs on the
//     resolution cache and a system token of its own, so the counter knows whose
//     door is being knocked on without the request having opened a transaction.
//     The second half is what keeps a refused write from being a transaction that
//     rolled back an attempt nobody made, and why the count survives the refusal
//     that follows it: it is written on a detached context, on kit/limit's budget.
//   - Safe methods are never counted. A crawler that reads a thousand pages is
//     not what a limit is for, and counting reads would make the limit a
//     function of traffic rather than of abuse.
//   - A limiter that cannot answer is an outage of the limiter's, not a denial
//     of the caller's: the request proceeds and one line is logged. A public
//     form that stops working because the counters table blinked is a form that
//     stops working, and the honest failure mode of a limit is the traffic it
//     lets through, not the traffic it invents refusals for.
//
// The refusal is answered through refuse, because the caller this limit exists
// for has no account to be locked out of and no terminal to read a code in: they
// are at a form in a browser, and the answer they are given is a page that says
// as much and a Retry-After that says whether to wait.
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
	key := publicWriteKey + " " + t.Slug + " " + ctx.Method() + " " + routeOf(ctx) + " " + clientAddress(r)
	ok, retryAfter, err := a.opts.WriteLimiter.Allow(context.WithoutCancel(ctx.Context()), key, publicWriteLimit, publicWriteWindow)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the public write limit could not be read; proceeding",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		next(ctx)
		return
	}
	if ok {
		next(ctx)
		return
	}
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: public write refused by the limit",
		"method", ctx.Method(), "path", ctx.URL().Path, "tenant", t.Slug)
	ctx.SetHeader("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
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

// clientAddress is the peer address of the request, which is the only address
// here that a caller cannot write: no X-Forwarded-For, for the reason the auth
// module gives for the same rule. A deployment behind a proxy that rewrites
// RemoteAddr gets the right one anyway.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
