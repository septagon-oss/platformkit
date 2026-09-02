// Package tenancy is the one tenant model and how a request carries it.
//
// A tenant reaches the rest of the program through the context: HTTP middleware
// resolves the host to a Tenant and calls WithTenant, and kit/db reads it back
// to scope the transaction. Nothing else may derive a tenant from anywhere.
package tenancy

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

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

// Resolver maps an incoming host to a tenant. Implemented by the tenant module.
type Resolver interface {
	ByHost(ctx context.Context, host string) (Tenant, error)
}

// SystemToken is a capability that lets kernel runners open cross-tenant work.
// Its only constructor lives in kit/internal/syscap, so packages outside kit/
// can name a token but cannot mint one: the compiler enforces that modules
// never bypass tenancy.
type SystemToken = syscap.SystemToken
