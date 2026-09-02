package httpx

import (
	"context"

	"github.com/google/uuid"
)

// Principal is who is making a request. It is established once per request by
// Options.Authenticate and never derived again: a handler that wants to know
// the caller reads it from the context, and nothing else may put one there.
type Principal struct {
	// UserID identifies the person or machine account.
	UserID uuid.UUID
	// TenantID is the tenant the credential belongs to. The authorization
	// middleware refuses a principal whose tenant is not the one the request
	// host resolved to, so a session for one tenant cannot act on another.
	TenantID uuid.UUID
	// Roles are the roles the credential carries, for an Authorizer that wants
	// them without a second lookup.
	Roles []string
}

// principalKey is unexported so only this package can put a principal on a
// context, the same discipline kit/tenancy applies to the tenant.
type principalKey struct{}

// WithPrincipal returns a copy of ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal ctx carries, if any. Absence means the
// caller is anonymous, which only a Public operation serves.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
