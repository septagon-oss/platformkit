package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// txKey carries the request's transaction. It is unexported, so TxFrom is the
// only way to reach it and the transaction middleware is the only way to set it.
type txKey struct{}

// TxFrom returns the tenant transaction the request is running in. It is
// present for every request whose host resolved to a tenant, and absent for a
// public request that resolved to none.
//
// This is how a handler reaches the database: there is no other door, and a
// repository that takes db.Tx[db.Tenant] cannot be called without going through
// one.
func TxFrom(ctx context.Context) (db.Tx[db.Tenant], bool) {
	tx, ok := ctx.Value(txKey{}).(db.Tx[db.Tenant])
	return tx, ok
}

// errRollback asks db.Run to roll back a transaction whose request failed. It
// never leaves this package: db.Run returns it, transaction recognises it, and
// the response has already been written by then.
var errRollback = errors.New("httpx: response failed, rolling back")

// authenticate establishes the caller's principal before routing. A hook that
// reports false leaves the context anonymous, which is not an error here: only
// the authorization middleware knows whether the operation minds.
func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := a.opts.Authenticate(r); ok {
			r = r.WithContext(WithPrincipal(r.Context(), p))
		}
		next.ServeHTTP(w, r)
	})
}

// tenant resolves the request host to a tenant and puts it on the context,
// where kit/db reads it.
//
// A host that names no tenant is a 404 rather than a 400: from outside, a site
// that is not served and a site that does not exist are the same fact. The one
// exception is an operation that declared itself Public, which may legitimately
// be reached at a host the resolver knows nothing about — a health probe
// addressing the pod by IP, say — and then proceeds with no tenant and, below,
// no transaction.
func (a *API) tenant(ctx huma.Context, next func(huma.Context)) {
	host := hostOnly(ctx.Host())
	t, err := a.opts.Tenants.ByHost(ctx.Context(), host)
	if err == nil && t.ID == uuid.Nil {
		// A resolver that answers with the zero Tenant has resolved nothing.
		// Taking it at its word would scope the request's transaction to the nil
		// UUID and, worse, make every zero Principal a member of that tenant.
		err = fmt.Errorf("the resolver returned the zero tenant for %q", host)
	}
	if err == nil {
		next(huma.WithContext(ctx, tenancy.WithTenant(ctx.Context(), t)))
		return
	}
	if auth, ok := declarationOf(ctx.Operation()); ok && auth.kind == kindPublic {
		next(ctx)
		return
	}
	a.log.DebugContext(ctx.Context(), "httpx: host resolves to no tenant", "host", host, "error", err)
	_ = huma.WriteErr(a, ctx, http.StatusNotFound, "no site is served at "+host)
}

// hostOnly is the resolver's key: the Host header without its port, lower-cased,
// without the trailing dot a fully qualified name may carry. Normalising here
// means every Resolver is spared doing it, and doing it differently.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// transaction runs the rest of the request inside one tenant transaction, and
// puts it where TxFrom finds it. A request that resolved to no tenant — only a
// public one reaches here — runs without a transaction, because db.Run has no
// tenant to scope one to.
//
// A response of 400 or worse rolls back. The response itself is already on the
// wire by then: huma writes the status as soon as the handler returns, so the
// client sees the failure it caused and the database keeps none of the work.
// The converse case, a commit that fails after a 200 was written, is logged and
// cannot be taken back; buffering every response to close that window would
// break streaming and is not worth its price here.
func (a *API) transaction(ctx huma.Context, next func(huma.Context)) {
	if _, ok := tenancy.FromContext(ctx.Context()); !ok {
		next(ctx)
		return
	}
	entered := false
	err := db.Run(ctx.Context(), a.opts.Conn, func(rctx context.Context, tx db.Tx[db.Tenant]) error {
		entered = true
		// huma.WithValue would rebuild the context from ctx and drop rctx,
		// which is the one carrying the open transaction, so the handle and the
		// transaction it belongs to are placed together here.
		inner := huma.WithContext(ctx, context.WithValue(rctx, txKey{}, tx))
		next(inner)
		if inner.Status() >= http.StatusBadRequest {
			return errRollback
		}
		return nil
	})
	switch {
	case err == nil || errors.Is(err, errRollback):
	case !entered:
		// Nothing was written, so the failure can still be reported honestly.
		a.log.ErrorContext(ctx.Context(), "httpx: could not open the request transaction", "error", err)
		_ = huma.WriteErr(a, ctx, http.StatusInternalServerError, "")
	default:
		a.log.ErrorContext(ctx.Context(), "httpx: request transaction failed after the response was written",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
	}
}

// authorize enforces the operation's declaration.
//
// Every refusal is a 403, never a 401: which of the four conditions failed says
// nothing an attacker can act on, but it says a great deal to whoever is
// debugging a deployment, so the code is in the response and the request that
// carried it is in the log.
func (a *API) authorize(ctx huma.Context, next func(huma.Context)) {
	auth, ok := declarationOf(ctx.Operation())
	if !ok {
		// Defense in depth. kit/app runs ValidateDeclarations before it
		// listens, so reaching this branch means an operation was mounted after
		// the gate ran.
		a.deny(ctx, "AUTH_UNDECLARED", "this operation declares no authorization")
		return
	}
	if auth.kind == kindPublic {
		next(ctx)
		return
	}

	p, hasPrincipal := PrincipalFrom(ctx.Context())
	if !hasPrincipal || p.UserID == uuid.Nil {
		a.deny(ctx, "AUTH_ANONYMOUS", "this operation requires a signed-in caller")
		return
	}
	t, hasTenant := tenancy.FromContext(ctx.Context())
	if !hasTenant {
		a.deny(ctx, "AUTH_NO_TENANT", "this operation is tenant work and the host resolved to none")
		return
	}
	if p.TenantID != t.ID {
		a.deny(ctx, "AUTH_TENANT_MISMATCH", "the principal belongs to another tenant than the host serves")
		return
	}
	if auth.kind == kindSignedIn {
		next(ctx)
		return
	}

	allowed, err := a.opts.Authorize.Allowed(ctx.Context(), t, auth.permission)
	if err != nil {
		// An authorization decision that could not be made is not a denial, and
		// saying so would send a person away from work they are entitled to do.
		a.log.ErrorContext(ctx.Context(), "httpx: authorization decision unavailable",
			"permission", auth.permission, "tenant", t.Slug, "error", err)
		ctx.SetHeader("Retry-After", "3")
		_ = huma.WriteErr(a, ctx, http.StatusServiceUnavailable, "authorization is temporarily unavailable")
		return
	}
	if !allowed {
		a.deny(ctx, "AUTH_DENIED", "this operation requires "+auth.permission)
		return
	}
	next(ctx)
}

// deny logs the machine-readable reason and answers 403 with it. The code is
// the first word of the detail so a log line and a response can be matched
// without adding a field to the one error shape kit/problem defines.
func (a *API) deny(ctx huma.Context, code, detail string) {
	a.log.InfoContext(ctx.Context(), "httpx: authorization denied",
		"code", code, "method", ctx.Method(), "path", ctx.URL().Path)
	_ = huma.WriteErr(a, ctx, http.StatusForbidden, code+": "+detail)
}
