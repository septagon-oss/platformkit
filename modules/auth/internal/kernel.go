package internal

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// Authenticate is kit/httpx's identity hook: the one query that turns a cookie
// into a caller.
//
// It runs after the host resolved and inside that tenant's transaction, so the
// session it looks for is a row of that tenant and nothing else. A session
// issued on one customer's host and presented on another's is simply not
// returned — row-level security, not a comparison — and the caller is anonymous.
//
// A cookie that is not a uuid, or names a session that has expired or belongs to
// a user who is no longer active, is anonymous rather than an error: none of
// those is an outage, and answering 500 to somebody with a stale cookie would
// make signing out of a deleted account look like a broken deployment. A
// database that cannot be read is an error, and kit/httpx answers 500.
func (s *Service) Authenticate(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
	cookie, ok := httpx.SessionCookieOf(r)
	if !ok {
		return tenancy.Principal{}, false, nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return tenancy.Principal{}, false, nil
	}
	identity, err := s.Identify(ctx, tx, id, ClientOf(r))
	if errors.Is(err, crud.ErrNotFound) {
		return tenancy.Principal{}, false, nil
	}
	if err != nil {
		return tenancy.Principal{}, false, err
	}
	return tenancy.Principal{UserID: identity.UserID, Roles: identity.Roles}, true, nil
}

// Allowed is kit/httpx's authorizer: one query per request that asks a
// permission question, in the request's own transaction, with no cache.
//
// No cache is the decision. A permission cache is a window in which a revoked
// grant still works, and what it would save is a primary-key read of a handful
// of rows — so the cost of being exactly right is one round trip on the
// requests that need one, and requests by an anonymous caller or by a caller
// with no roles do not even make that.
//
// The tenant is unused, and deliberately: the kernel has already refused an
// operator grant on a tenant that is not the operator's, and every row this
// reads is inside that tenant's own transaction. A second comparison here would
// be a check that cannot fail.
func (s *Service) Allowed(ctx context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	principal, ok := tenancy.PrincipalFrom(ctx)
	if !ok || len(principal.Roles) == 0 {
		return false, nil
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return false, errors.New("auth: no transaction to resolve the caller's permissions in")
	}
	held, err := s.Permissions(ctx, tx, principal.Roles)
	if err != nil {
		return false, err
	}
	return contracts.Grants(held, grant), nil
}

var _ httpx.Authorizer = (*Service)(nil)

// ClientOf is what a session records about where it was opened.
func ClientOf(r *http.Request) contracts.Client {
	if r == nil {
		return contracts.Client{}
	}
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	// No X-Forwarded-For. A header a client can write is not an address, and a
	// deployment behind a proxy that rewrites RemoteAddr gets the right one
	// anyway; trusting the header here would let anybody record any address in
	// somebody else's session list.
	return contracts.Client{UserAgent: r.UserAgent(), IP: ip}
}

// Cookies mints and clears this module's two cookies.
//
// Secure is on unless the application is reached at a local name, because a
// browser refuses a Secure cookie over http://localhost and a development
// machine that cannot sign in is a development machine nobody uses. The name
// follows from it: httpx.CookieName adds the __Host- prefix when the cookie
// will be Secure, which is what stops a page served at one customer's host
// setting a session cookie the browser then attaches at another's. Every tenant
// is reached at its own host, often as siblings under one domain, so that is
// not a hypothetical here.
//
// SameSite is Lax rather than Strict so that following a link into the
// application from somewhere else does not land on a signed-out page; the
// cross-site writes Lax still allows are what kit/httpx's CSRF middleware
// refuses. Path is "/" on both, because __Host- requires it — the state cookie
// used to be scoped to this module's own prefix, and the prefix is worth less
// than a cookie a sibling host cannot forge.
type Cookies struct{ secure bool }

// NewCookies returns the cookie policy for an application reached at publicHost.
func NewCookies(secure bool) Cookies { return Cookies{secure: secure} }

// Session is the cookie that carries a session id. The value is the credential
// and nothing stores it; what the row holds is its hash.
func (c Cookies) Session(id uuid.UUID, expires time.Time) http.Cookie {
	return c.set(httpx.SessionCookie, id.String(), int(time.Until(expires).Seconds()))
}

// Clear is the cookie that removes the session: same name, same path, no value,
// expired. The attributes have to match or the browser keeps the one it has.
func (c Cookies) Clear() http.Cookie { return c.set(httpx.SessionCookie, "", -1) }

// State is the OIDC state cookie, and Forget removes it.
func (c Cookies) State(value string, seconds int) http.Cookie {
	return c.set(stateCookie, value, seconds)
}

// Forget expires a cookie this policy set.
func (c Cookies) Forget(base string) http.Cookie { return c.set(base, "", -1) }

// Name is what a cookie this policy sets is called, so a handler reading one
// back spells it the way it was written.
func (c Cookies) Name(base string) string { return httpx.CookieName(base, c.secure) }

func (c Cookies) set(base, value string, maxAge int) http.Cookie {
	return http.Cookie{
		Name: httpx.CookieName(base, c.secure), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	}
}
