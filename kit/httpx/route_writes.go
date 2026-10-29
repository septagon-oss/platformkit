package httpx

// route_writes.go bounds what an authenticated caller may submit, on the operations that say
// their writes are counted: per person and per tenant, in a window, refused with 429 and a
// Retry-After.
//
// It is a sibling of public_writes.go and not a generalisation of it. The anonymous arm
// counts an address because an anonymous visitor has nothing else to count, and runs ahead of
// the transaction because it must not need one; an authenticated route has a person, a
// permission and a body not yet read, and a key naming an address would count the office
// proxy rather than the caller. Both count the same way and both answer with Retry-After.
//
// Two counters per operation, evaluated in the order declared and stopping at the first spent
// one, so a caller already over their own allowance does not also spend the tenant's bucket;
// the actor entry is skipped when the request carries no principal. A limiter that cannot
// answer lets the request through and logs, for the reason the anonymous arm gives.

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// routeWriteKey prefixes every key this arm counts under, in the shape
// publicWriteKey takes. The tenant and the application are already in front of it:
// kit/limit scopes every key it stores by both, so no key here can forge a tenant.
const routeWriteKey = "httpx/route-write"

// WriteLimitExtension marks an operation whose writes are counted, per actor and
// per tenant, by Options.WriteLimiter. It is an extension and not a field on
// Options for StreamedBody's reason: the kernel must not know which route is an
// upload, a send or an import.
const WriteLimitExtension = "x-write-limit"

// The two identities a write can be counted against. Anything else — an address,
// an API key, a user agent — is either the Public surface's subject or nobody's.
const (
	LimitPerActor  = "actor"
	LimitPerTenant = "tenant"
)

// WriteLimit is one counted window on one operation, in the shape the OpenAPI
// document carries so that the bound a caller must respect is in the same document
// as the route it bounds. Window is Go's own duration spelling ("1m"), the way
// this repository spells every duration a file declares.
type WriteLimit struct {
	Per    string `json:"per"`    // LimitPerActor or LimitPerTenant
	Limit  int    `json:"limit"`  // submissions allowed per window
	Window string `json:"window"` // Go's duration spelling: "1m"
}

// WriteLimits is what a route that wants its writes counted puts in its
// Extensions, beside StreamedBody and EventsExtension.
func WriteLimits(limits ...WriteLimit) (string, any) { return WriteLimitExtension, limits }

// declaredWriteLimits reads the declaration back. Absent, empty or unreadable is
// "this operation is not counted": the arm then costs one map lookup. A
// declaration that parses but is wrong is refused at boot by
// ValidateDeclarations, so nothing here has to guess what a bad one meant.
func declaredWriteLimits(op *huma.Operation) []WriteLimit {
	if op == nil || op.Extensions == nil {
		return nil
	}
	raw, ok := op.Extensions[WriteLimitExtension].([]WriteLimit)
	if !ok {
		return nil
	}
	return raw
}

// writeLimits counts the declared operations of the authenticated surface. It
// runs after a.authorize — so a caller who may not perform the write never spends
// the allowance, and a 429 is never anybody's first lesson in which permission
// they lack — and before a.bodies, so no byte of the request has been read and a
// refusal rolls the guards' transaction back untouched, which is what an
// authorisation denial already does.
func (a *API) writeLimits(ctx huma.Context, next func(huma.Context)) {
	limits := declaredWriteLimits(ctx.Operation())
	if len(limits) == 0 || !unsafeMethod(ctx.Method()) {
		next(ctx)
		return
	}
	if a.opts.WriteLimiter == nil {
		// The declaration is a route's promise and the limiter is the
		// deployment's; when only one of them exists, the route keeps working
		// and says so on every counted request rather than refusing traffic it
		// has no way to count.
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: an operation counts its writes and Options.WriteLimiter is nil",
			"operation", opName(ctx.Operation()), "method", ctx.Method(), "path", ctx.URL().Path)
		next(ctx)
		return
	}
	for _, limit := range limits {
		key, count := writeLimitKey(ctx, limit.Per)
		if !count {
			continue
		}
		window, err := time.ParseDuration(limit.Window)
		if err != nil {
			// Unreachable while ValidateDeclarations ran: a window that does not
			// parse is a deployment's own typo and it stopped the boot. Treated
			// as the other arm treats a limiter that cannot answer.
			a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: a declared write window does not parse; proceeding",
				"operation", opName(ctx.Operation()), "window", limit.Window, "error", err)
			next(ctx)
			return
		}
		allowed, _, retryAfter, err := a.opts.WriteLimiter.Reserve(context.WithoutCancel(ctx.Context()), key, limit.Limit, window)
		if err != nil {
			a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the write limit could not be read; proceeding",
				"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
			next(ctx)
			return
		}
		if allowed {
			continue
		}
		a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: write refused by the limit",
			"method", ctx.Method(), "path", ctx.URL().Path, "per", limit.Per, "limit", limit.Limit)
		sayRetryAfter(ctx, retryAfter)
		a.refuse(ctx, http.StatusTooManyRequests,
			CodeLimitExhausted+": too many submissions "+writeLimitSays(limit.Per))
		return
	}
	next(ctx)
}

// writeLimitKey is the counter this entry reads. The route half is the mounted
// pattern and not the request's path, which is routeOf's whole argument and holds
// here for the same reason: a set of counters anyone can inflate is a table anyone
// can fill.
func writeLimitKey(ctx huma.Context, per string) (key string, count bool) {
	base := routeWriteKey + " " + ctx.Method() + " " + routeOf(ctx)
	switch per {
	case LimitPerTenant:
		return base + " tenant", true
	case LimitPerActor:
		p, ok := tenancy.PrincipalFrom(ctx.Context())
		if !ok || p.UserID == uuid.Nil {
			// Nobody to count: the Public arm's subject, keyed by address.
			return "", false
		}
		return base + " actor " + p.UserID.String(), true
	}
	return "", false
}

// writeLimitSays names whose allowance ran out, because the answer a tenant-wide
// fuse gives is not "you did this".
func writeLimitSays(per string) string {
	if per == LimitPerTenant {
		return "for this tenant"
	}
	return "from this caller"
}

// opName is the operation half of a log line: the id the route was registered
// with, which is what a reader of the boot document searches for.
func opName(op *huma.Operation) string {
	if op == nil || op.OperationID == "" {
		return "(unnamed)"
	}
	return op.OperationID
}

// checkWriteLimits refuses a declaration no arm could act on: an identity that is
// neither of the two, a ceiling nobody could reach because it is not a number of
// submissions, or a window that is not a duration. kit/app runs this before it
// serves, so a typo in a limit is a failed boot rather than a route that quietly
// counts nothing — a limit nobody can read is worse than no limit.
func checkWriteLimits(describe string, op *huma.Operation) string {
	for _, limit := range declaredWriteLimits(op) {
		switch {
		case limit.Per != LimitPerActor && limit.Per != LimitPerTenant:
			return describe + " names " + limit.Per + " for a counted write, not " +
				LimitPerActor + " or " + LimitPerTenant
		case limit.Limit <= 0:
			return describe + " allows " + strconv.Itoa(limit.Limit) + " submissions per window"
		}
		if _, err := time.ParseDuration(limit.Window); err != nil {
			return describe + " declares the window " + limit.Window + ", which is not a duration"
		}
	}
	return ""
}
