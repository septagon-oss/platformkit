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
	// administrators may reach the control plane. A customer's administrator
	// holds "may do everything in my tenant", which must not become the power
	// to list, create and suspend the tenants beside them. The column is
	// written by the bootstrap and by nothing else; see Grant and docs/adr/0006.
	Operator bool
}

// Grant is a permission question: which token, and whether that token is one
// only the operator's own tenant may exercise.
//
// It is a struct rather than a string because "may this caller do X" and "is
// this a tenant entitled to X at all" are two questions and the second is asked
// first. It lives beside Tenant because both halves of the answer are here.
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

// Principal is who is making a request, established once per request by the
// HTTP layer's identity hook and never derived again.
//
// It lives here, beside Tenant and the actor, because the three are one thing.
// It used to live in kit/httpx, and the cost was a module's contracts/ package
// naming the HTTP kernel to describe its own caller — which linked huma and chi
// into the build graph of anything that only wanted to name a Session.
//
// There is no tenant on it: the hook runs inside the resolved tenant's own
// transaction, so a credential belonging to another is a row row-level security
// does not show, and the principal that would have had to be refused is never
// built.
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
