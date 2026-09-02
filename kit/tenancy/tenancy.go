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
	// Operator says this tenant is the installation's own, the one whose
	// administrators may reach the control plane. Every other tenant is a
	// customer, and a customer's administrator holding "may do everything in
	// my tenant" must not thereby be able to list, create or suspend the
	// tenants beside it. The column is written by the bootstrap and by nothing
	// else; see Grant and docs/adr/0006.
	Operator bool
}

// Grant is a permission question: which token, and whether that token is one
// only the operator's own tenant may exercise.
//
// It is a struct rather than a string because "may this caller do X" and "may
// this caller do X, here, in a tenant entitled to X at all" are two questions
// and the second one has to be asked first. It lives beside Tenant because both
// halves of the answer are here: the flag on the permission and the flag on the
// tenant.
type Grant struct {
	Permission string
	Operator   bool
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

// Principal is who is making a request. It is established once per request by
// the HTTP layer's identity hook and never derived again: a handler that wants
// to know the caller reads it from the context, and nothing else may put one
// there.
//
// It lives here, beside Tenant and the actor, because the three are one thing:
// the identity of a request is which customer it is about, who is asking, and
// what the asking was recorded as. It used to live in kit/httpx, and the cost
// of that was a module's contracts/ package naming the HTTP kernel to describe
// its own caller — which linked huma and chi into the build graph of anything
// that only wanted to name a Session.
//
// There is no tenant on it. The hook runs after the host has resolved and
// inside that tenant's own transaction, so a credential belonging to another
// tenant is a row row-level security does not show — the principal that would
// have had to be refused is a principal that is never built. A field the
// middleware could only ever compare with the tenant it just set itself is a
// check that proves nothing.
type Principal struct {
	// UserID identifies the person or machine account.
	UserID uuid.UUID
	// Roles are the roles the credential carries, for an authorizer that wants
	// them without a second lookup.
	Roles []string
}

// principalKey is unexported for the same reason contextKey is.
type principalKey struct{}

// WithPrincipal returns a copy of ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal ctx carries, if any. Absence means the
// caller is anonymous, which only a public operation serves.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// actorKey is unexported for the same reason contextKey is: only this package
// can put an actor on a context, and only kit/httpx calls WithActor.
type actorKey struct{}

// WithActor returns a copy of ctx carrying the user who is making the request.
//
// It lives here, beside the tenant, because the two are one thing: the identity
// of a request is which customer it is about and who is asking. kit/httpx calls
// it once, immediately after Options.Authenticate recognises the caller, and
// kit/events reads it back so that every event an operation writes carries the
// person who caused it without any module remembering to pass it along.
//
// Work with no person behind it — a job, the relay, an event handler — leaves
// it unset, and that is the honest answer rather than a sentinel: the outbox
// column is null and the audit row says the system did it.
func WithActor(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorKey{}, userID)
}

// ActorFrom returns the user ctx carries, if any. The nil UUID is not an actor:
// a zero principal must not read as somebody.
func ActorFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(actorKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
