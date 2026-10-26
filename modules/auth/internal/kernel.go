package internal

import (
	"context"
	"errors"
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
	if token, ok := httpx.BearerOf(r); ok {
		return s.bearerPrincipal(ctx, tx, token)
	}
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

// bearerPrincipal is the same recognition arrived at the other way: not what the
// browser was given, but what the caller was handed. It carries the intersection
// of the key's scopes and what its holder's roles still grant, and it carries no
// roles at all — see tenancy.Principal.Permissions for why a scoped credential
// with a role list beside it would be a key that ignores its own scope.
//
// A key that is revoked, expired, belongs to a person who is no longer active, or
// was never issued here is one answer: not recognised, no error, nothing written.
// A revoked key that answered "revoked" would be a probe that told its holder the
// revocation had landed.
func (s *Service) bearerPrincipal(ctx context.Context, tx db.Tx[db.Tenant], token string) (tenancy.Principal, bool, error) {
	cred, ok, err := s.bearerCaller(ctx, tx, token)
	if err != nil || !ok {
		return tenancy.Principal{}, false, err
	}
	return tenancy.Principal{UserID: cred.user.ID, TokenID: cred.tokenID, Permissions: cred.scopes}, true, nil
}

// Allowed delegates live requests to the injected transaction-scoped authorizer.
// Legacy calls without a request transaction can only decide without a role read.
func (s *Service) Allowed(ctx context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	if tx, ok := httpx.TxFrom(ctx); ok {
		return s.authorization.Allowed(ctx, tx, grant)
	}
	principal, _ := tenancy.PrincipalFrom(ctx)
	return contracts.Decide(principal, grant, func([]string) ([]string, error) {
		return nil, errors.New("auth: no transaction to resolve the caller's permissions in")
	})
}

var _ httpx.Authorizer = (*Service)(nil)

// ClientOf is what a session records about where it was opened.
func ClientOf(r *http.Request) contracts.Client {
	if r == nil {
		return contracts.Client{}
	}
	// No X-Forwarded-For. A header a client can write is not an address, and a
	// deployment behind a proxy that rewrites RemoteAddr gets the right one
	// anyway; trusting the header here would let anybody record any address in
	// somebody else's session list. The parse itself is kit/httpx's — one parser
	// in the tree, for the rule two modules used to spell out separately.
	return contracts.Client{UserAgent: r.UserAgent(), IP: httpx.ClientAddr(r)}
}

// Cookies mints and clears this module's two cookies.
//
// Secure is on unless the application is reached at a local name, because a
// browser refuses a Secure cookie over http://localhost and a development
// machine that cannot sign in is a development machine nobody uses. The name
// follows from it: httpx.CookieNameFor adds the __Host- prefix when the cookie
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
//
// Its lifetime is the one the session it names could still have — SessionMaxLifetime
// from the moment it opened — and not the expiry the row happened to carry at
// sign-in. The row slides on use and the server, not the browser, decides whether a
// session is live; a cookie that expired with that expiry would be gone first in the
// two cases worth naming, the session that ran out while its person was away and the
// one used enough to be slid past the moment the cookie said. A server handed no id
// then cannot tell the person who was signed in from somebody arriving for the first
// time, and greets them as a stranger. An id whose row is gone names no session and
// grants nothing, and signing out clears this cookie whatever its expiry says. The
// seconds are rounded up, so the cookie outlives the cap it is meant to outlive.
func (c Cookies) Session(id uuid.UUID, openedAt time.Time) http.Cookie {
	return c.set(httpx.SessionCookie, id.String(), int(time.Until(openedAt.Add(contracts.SessionMaxLifetime))/time.Second)+1)
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
func (c Cookies) Name(base string) string { return httpx.CookieNameFor("", base, c.secure) }

func (c Cookies) set(base, value string, maxAge int) http.Cookie {
	return http.Cookie{
		Name: httpx.CookieNameFor("", base, c.secure), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	}
}
