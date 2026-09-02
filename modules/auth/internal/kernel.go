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
func (s *Service) Authenticate(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (httpx.Principal, bool, error) {
	cookie, err := r.Cookie(httpx.SessionCookie)
	if err != nil {
		return httpx.Principal{}, false, nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return httpx.Principal{}, false, nil
	}
	identity, err := s.Identify(ctx, tx, id, ClientOf(r))
	if errors.Is(err, crud.ErrNotFound) {
		return httpx.Principal{}, false, nil
	}
	if err != nil {
		return httpx.Principal{}, false, err
	}
	return httpx.Principal{UserID: identity.UserID, Roles: identity.Roles}, true, nil
}

// Allowed is kit/httpx's authorizer: one query per request that asks a
// permission question, in the request's own transaction, with no cache.
//
// No cache is the decision. A permission cache is a window in which a revoked
// grant still works, and what it would save is a primary-key read of a handful
// of rows — so the cost of being exactly right is one round trip on the
// requests that need one, and requests by an anonymous caller or by a caller
// with no roles do not even make that.
// The tenant is unused, and deliberately: the kernel has already refused an
// operator grant on a tenant that is not the operator's, and every row this
// reads is inside that tenant's own transaction. A second comparison here would
// be a check that cannot fail.
func (s *Service) Allowed(ctx context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	principal, ok := httpx.PrincipalFrom(ctx)
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

// Cookies mints and clears the session cookie.
//
// Secure is on unless the application is reached at a local name, because a
// browser refuses a Secure cookie over http://localhost and a development
// machine that cannot sign in is a development machine nobody uses. SameSite is
// Lax rather than Strict so that following a link into the application from
// somewhere else does not land on a signed-out page; the cross-site writes Lax
// still allows are what kit/httpx's CSRF middleware refuses.
type Cookies struct{ secure bool }

// NewCookies returns the cookie policy for an application reached at publicHost.
func NewCookies(secure bool) Cookies { return Cookies{secure: secure} }

// Session is the cookie that carries a session id.
func (c Cookies) Session(id uuid.UUID, expires time.Time) http.Cookie {
	return http.Cookie{
		Name: httpx.SessionCookie, Value: id.String(), Path: "/",
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	}
}

// Clear is the cookie that removes it: same name, same path, no value, expired.
// The attributes have to match or the browser keeps the one it has.
func (c Cookies) Clear() http.Cookie {
	return http.Cookie{
		Name: httpx.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	}
}
