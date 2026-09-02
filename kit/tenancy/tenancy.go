// Package tenancy is the one tenant model and how a request carries it.
//
// A tenant reaches the rest of the program through the context: HTTP middleware
// resolves the host to a Tenant and calls WithTenant, and kit/db reads it back
// to scope the transaction. Nothing else may derive a tenant from anywhere.
package tenancy

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// ErrNoSuchHost is what a tenant loader returns when it looked and there is no
// site at that host. It is the one loader failure that is an answer rather than
// an outage: the HTTP layer turns it into 404 and everything else into 503, so
// a database the loader cannot reach never reads as "these hosts do not exist".
var ErrNoSuchHost = errors.New("tenancy: no tenant at this host")

// Tenant is one customer of the platform.
type Tenant struct {
	ID   uuid.UUID
	Slug string
	Name string
}

// contextKey is unexported so only this package can put a tenant on a context.
type contextKey struct{}

// WithTenant returns a copy of ctx carrying t.
func WithTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, contextKey{}, t)
}

// FromContext returns the tenant ctx carries, if any.
func FromContext(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(contextKey{}).(Tenant)
	return t, ok
}

// SystemToken is a capability that lets kernel runners open cross-tenant work.
// It is an interface whose only implementation and only constructor live in
// kit/internal/syscap, so packages outside kit/ can name a token but can
// neither mint nor implement one: the compiler enforces that modules never
// bypass tenancy.
//
// Resolving a host to a tenant is a query, so it needs a transaction; the
// interface for it is httpx.TenantLoader, declared where both kit/db and this
// package are already imported.
type SystemToken = syscap.SystemToken
