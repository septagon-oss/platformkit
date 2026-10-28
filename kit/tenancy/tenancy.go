// Package tenancy is the one tenant model and how a request carries it.
//
// A tenant reaches the rest of the program through the context: HTTP middleware
// resolves the host to a Tenant and calls WithTenant, and kit/db reads it back
// to scope the transaction. Nothing else may derive a tenant from anywhere.
//
// The [Policy] example evaluates a tenant-qualified request through [RequirePolicy].
// It shows an allowed decision and a denial from an explicit provider.
package tenancy

import (
	"context"
	"errors"
	"regexp"

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
	// Demo is a creation-time tenant property. A seed request cannot upgrade an
	// ordinary tenant into a walkthrough tenant by passing a flag.
	Demo bool
	// Languages are what this tenant is served in, or nil when the tenant declared
	// nothing and the deployment's own default answers. The host resolution reads
	// them with the rest of the row, which is what makes "which language is this
	// request answered in" a fact about the tenant resolved for *this* request
	// rather than a property of the catalog fixed at composition.
	//
	// A pointer rather than a slice so that Tenant stays comparable: three owners
	// of a resolution ask "is this the tenant I resolved before?" with ==, and a
	// struct that cannot answer that question would make them compare fields by
	// hand — which is how a field gets forgotten.
	Languages *Languages
}

// Languages are the languages one tenant is served in, as the control plane
// declares them: one default, and the set a browser's list is intersected with.
//
// It is a value type with no behaviour, and the order inside it is the whole of
// what a fallback means: Default first when nothing in the request's own list is
// served here, and Others never ranking against one another.
type Languages struct {
	// Default is the language a request that brought nothing this tenant serves
	// is answered in.
	Default string
	// Others are the remaining languages this tenant is served in, in no
	// particular order.
	Others []string
}

// Preferred is the tenant's language list in the one order a negotiation reads:
// the default, then the rest. With no default set it is the rest alone.
func (l *Languages) Preferred() []string {
	if l == nil {
		return nil
	}
	if l.Default == "" {
		return l.Others
	}
	out := make([]string, 0, len(l.Others)+1)
	out = append(out, l.Default)
	for _, tag := range l.Others {
		if tag != l.Default {
			out = append(out, tag)
		}
	}
	return out
}

// Grant is a permission question: which token, and whether that token is one
// only the operator's own tenant may exercise.
//
// It is a struct rather than a string because "may this caller do X" and "is
// this a tenant entitled to X at all" are two questions and the second is asked
// first. It lives beside Tenant because both halves of the answer are here.
type Grant struct {
	Permission string
	// Label is the grant in words, carried from the manifest that defines it
	// (module.Permission.Label) so the layer that renders a refusal can name
	// what is missing without importing the module that owns the token. It
	// travels with the whole struct — httpx.Declare and Permissions move it to
	// every consumer without a further conversion.
	Label    string
	Operator bool
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

// Namespace is one module's namespace: the name the composition registered, and
// the name a workspace puts in front of that module's screens and its API. It is
// a type and not a string because the thing it carries is ownership — a module
// that mounts a page beside another module's screens should be holding a
// reference to the owner, not a name somebody remembered, and a rename of the
// owning module should break the caller's build rather than invent a namespace
// that answers 404.
//
// The value has to be nameable by an owning module's contracts package, which is
// why it lives here and not in kit/httpx: httpx links huma and chi into
// everything that imports it, and a module's contracts stay free of both. The
// kernel reads it on a router mount: httpx.Router.In.
type Namespace struct{ module string }

// NewNamespace names one module's own namespace. The owning module's contracts
// package calls it, from its own package, which is what makes the value a
// reference to an owner. A name that is not a module name panics at the
// definition: the value is written once, in source, and no request can bring it.
func NewNamespace(module string) Namespace {
	if !namespaceName.MatchString(module) {
		panic("tenancy.NewNamespace: a namespace is a module name — a lower-case identifier, the name the composition registered")
	}
	return Namespace{module: module}
}

// Module is the namespace's name, as the composition registered it.
func (n Namespace) Module() string { return n.module }

// String is the same name, so a namespace reads as itself in a refusal or a test
// failure instead of as the address of a struct.
func (n Namespace) String() string { return n.module }

// namespaceName is a module name's grammar — the same grammar kit/module
// refuses a manifest's Name against, restated here because kit/module imports
// this package. The two are kept in step by the case in this package's tests
// that asks module.Validate about the same names.
var namespaceName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

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
	// TokenID is the api key that authenticated this request, and uuid.Nil when
	// it was authenticated any other way: a session cookie, an in-process caller.
	// It names which credential is acting, never the credential's secret, and
	// it is what lets a key retire itself and no other.
	TokenID uuid.UUID
	// Roles are the roles the credential carries, for an authorizer that wants
	// them without a second lookup.
	Roles []string
	// Permissions is the authority the credential itself carries, and nil means
	// it carries none: the roles decide, which is every request that has ever
	// been served through here.
	//
	// A non-nil list is a ceiling, not an addition. An authorizer may grant what
	// is on this list and nothing else, whatever the holder's roles say, and the
	// roles are deliberately not consulted for such a caller: a scoped key is not
	// an administrator who happens to be asking politely, and a role list beside
	// it would be read by every role-shaped decision in the application as the
	// authority the scope exists to narrow. So a bearer carries no Roles — see
	// the auth module's bearer resolution, which hands over the intersection of
	// the key's scopes and the holder's grants and no roles at all.
	Permissions []string
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
// can put an actor on a context, and only the two callers named below do.
type actorKey struct{}

// WithActor returns a copy of ctx carrying the user who is making the request.
//
// It lives here, beside the tenant, because the two are one thing: the identity
// of a request is which customer it is about and who is asking. kit/httpx calls
// it when Options.Authenticate recognises a caller, and kit/events reads it back
// so every event names who caused it. A door that admits an anonymous request
// — a password, a passkey — calls it for the person it recognises inside its own
// transaction: the request arrived as nobody, or as whoever's cookie it carried.
//
// Work with no person behind it — a job, the relay, an event handler — leaves
// it unset: null in the outbox, and the trail says the system did it.
func WithActor(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorKey{}, userID)
}

// ActorFrom returns the user ctx carries, if any. The nil UUID is not an actor:
// a zero principal must not read as somebody.
func ActorFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(actorKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
