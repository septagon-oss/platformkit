package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TenantLoader maps an incoming host to a tenant. The tenant module implements
// it in E2; it returns tenancy.ErrNoSuchHost when there is simply no site at
// the host, and any other error when it could not tell.
//
// It takes a db.Tx[db.System] because the answer is a query and the row it
// looks for belongs to no tenant yet — the request has not resolved one. This
// package mints the capability, since no module can, and passes the transaction
// in: an implementation cannot open a cross-tenant transaction of its own, and
// the one it is handed lasts only for the call.
//
// It is declared here rather than in kit/tenancy because kit/db imports
// kit/tenancy; this package already imports both.
type TenantLoader interface {
	ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error)
}

// Authorizer decides whether the caller of a request may exercise a grant in a
// tenant. The auth module implements it; a test implements it in three lines.
// It is the only thing this package knows about roles. It runs inside the
// request, so an implementation that needs the tenant's own rows reaches them
// with TxFrom.
//
// The grant carries the operator flag as well as the permission, and the
// implementation is held to it: a wildcard must not satisfy an operator grant,
// because "everything in my tenant" is not "everything to every tenant". This
// package has already refused such a grant on a tenant that is not the
// operator's, so the question left is whether the caller's roles name it.
type Authorizer interface {
	Allowed(ctx context.Context, tenant tenancy.Tenant, grant tenancy.Grant) (bool, error)
}

// Entitler answers what the tenant's plan includes, for the operations that
// declare a feature with Auth.Needing. It is a second question and not a second
// authorizer: a permission is what a person may do, a feature is what their
// tenant is paying for, and conflating them makes a price change a permission
// change.
//
// An installation that sells nothing implements none of this: an application
// whose operations declare no feature is never asked.
type Entitler interface {
	Includes(ctx context.Context, tenant tenancy.Tenant, feature string) (bool, error)
}

// Options are the collaborators main chooses for the HTTP layer. Every field
// except Log, PublicHost and Docs is required: an API missing one of them could
// only fail closed on every request, which is worse than failing at New.
// Denial is one refused authorization, as Options.Denied receives it: who was
// refused, in which tenant, at which operation, and why — the code is the first
// word of the refusal's detail, the same machine-readable reason the response and
// the log line carry, and RequestID joins it to both.
type Denial struct {
	Status int
	Code   string
	Detail string
	Method string
	Path   string
	// Permission and Label are the grant that was missing, when the refusal was
	// about a grant: the trail has to answer "which permission did they lack"
	// without the request id and a log line.
	Permission string
	Label      string
	Operation  string
	RequestID  string
	Tenant     tenancy.Tenant
	Principal  tenancy.Principal
}

type Options struct {
	// PublicHost is the host the application believes it is reached at. It
	// names the server in the OpenAPI document; the tenant of a request always
	// comes from that request's own Host header, never from this.
	PublicHost string

	// JSONMediaTypes are the media types this composition reads and writes as
	// JSON, beside application/json. A protocol that defines its own type —
	// SCIM says application/scim+json — arrives with a Content-Type the API has
	// never heard of, and huma answers a body it cannot parse with 415 before
	// any route runs: the door exists and refuses every client that speaks it.
	// Registering the type here is the one place that can be said, because it is
	// the composition that knows which protocols it serves, not a module that
	// would have to reach the API to say so.
	//
	// Each entry has to be a media type of the form type/subtype or
	// type/subtype+suffix; anything else refuses the build. What is registered is
	// JSON — the same encoder and decoder application/json uses — so the type
	// changes the wire's name and never its grammar.
	JSONMediaTypes []string

	// Docs serves /openapi.json, /openapi.yaml and /docs. They are public and
	// unauthenticated by construction, and they publish every route and every
	// permission the application has, which is a map worth having before an
	// attack and worth withholding during one. The reference app turns them on;
	// a deployment that would rather not can turn them off. The JSON Schema
	// route stays either way, because response bodies link to it.
	Docs bool

	// Tenants maps a request host to a tenant.
	Tenants TenantLoader

	// Conn is the application connection every request transaction opens on.
	//
	// It may be nil exactly when Unwired is set. Every other build of an API is a
	// build that intends to serve, and serving without a connection is a request
	// that panics instead of a composition that was refused. An unwired build that
	// will serve gets its connection from Connect; one that will not is thrown away
	// once the gates have answered.
	Conn *db.Conn

	// Unwired records that this API is built while the deployment has opened
	// nothing: the routes are registered so the gates can read them, and the API is
	// then either thrown away (a gate pass, see app.Declarations) or handed its
	// connection and its store by Connect before anything can reach it (the build
	// that will serve). What the flag buys is the thing it is there for: a build
	// that answers a question about its own routes without a pool, a migration or a
	// dial of the store the deployment named — and, for the build that serves, the
	// same answer about the last registration of its routes rather than only the
	// first three. Kit/app is the only caller that sets it.
	Unwired bool

	// Authorize answers the permission questions the declarations ask.
	Authorize Authorizer

	// Entitle answers the plan questions the declarations ask. It is required
	// only when an operation declares a feature, and ValidateDeclarations
	// refuses a startup where one does and this is nil — a feature nothing can
	// answer is a door that would be either always open or always shut, and
	// both are wrong in a way nobody would notice until a customer did.
	Entitle Entitler

	// Authenticate recognises the caller. It runs after the host has resolved
	// to a tenant and is handed that request's own transaction, so a session
	// lookup is an ordinary tenant-scoped query under row-level security: the
	// auth module neither resolves the host a second time nor asks for a
	// capability to read across tenants, because a credential belonging to
	// somebody else's tenant is a row it cannot see.
	//
	// It reports false for an anonymous caller, which is not an error — a
	// Public operation serves them. An error is an outage: the request is a 500
	// and the reason is logged, because a session store that cannot be reached
	// must not read as "you are not signed in".
	Authenticate func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)

	// Fault renders a refusal as a document, for a request that came from a browser
	// rather than a client. It is here rather than inside the kernel because the kernel
	// knows the verdict and nothing about chrome, stylesheets or where "back" is; the
	// application registers ui/page's renderer (or its own) and every guard in this
	// package then answers a person with a page instead of a JSON body.
	//
	// Nil — the default — keeps the previous behaviour exactly: every kernel refusal is
	// an RFC 9457 problem+json document, including for a browser navigation.
	Fault Fault

	// Log receives the reason behind every denial and every rolled-back
	// transaction. Defaults to slog.Default().
	Log *slog.Logger

	// MaxUpload is the largest file this deployment accepts, and it bounds the
	// body of the one kind of route that reads its own request rather than
	// declaring a schema for it — see StreamedBody. The envelope a multipart
	// form wraps the file in is allowed for on top of it.
	//
	// Zero means MaxBodyBytes, which is what every other route gets: a
	// deployment that mounts no streaming route needs no larger number, and a
	// test that mounts one is not testing the ceiling.
	MaxUpload int64

	// Denied is told about every refused authorization that has somebody to
	// attribute it to — a signed-in principal in a resolved tenant — after the
	// refusal is written. The composition records it (kit/app publishes
	// security.denied, which modules/audit keeps), so a denial is an audit row
	// and an event rather than a log line. An anonymous refusal is not passed:
	// a stranger at the door has no account to attribute it to, and a hook
	// that wrote a row per anonymous request would turn every probe into a
	// database write. Nil records nothing. It must not panic and its error is
	// its own to report; the caller's answer is already decided.
	Denied func(ctx context.Context, d Denial)

	// Installation is the host the installation itself is reached at — the one
	// address that serves the control plane (the Ops surface). It is a
	// composition's fact and not a tenant's: no TenantLoader answers it, because
	// the host names the installation rather than a customer.
	//
	// Empty means the installation has named no host of its own. The Ops surface
	// still mounts, every request to it is a 404 exactly as an address nobody
	// mounted is, and boot says so once: a control plane nobody can reach is
	// worth a line in the log rather than a silent surprise on the first
	// incident.
	Installation string

	// WriteLimiter counts the anonymous writes of the Public surface, which are
	// the writes with no account to lock out. Nil leaves them uncounted — a
	// deployment that mounts nothing anonymous never needs one, and a test that
	// mounts one route is not testing a limit.
	//
	// The counter runs on a detached context, outside the request's transaction
	// and on the limiter's own budget: a refused public write must not be a
	// transaction that rolled back an attempt nobody made, and a counted one must
	// survive the refusal that followed it. That is the same argument the auth
	// module makes for its lockout.
	WriteLimiter WriteLimiter

	// Idempotency is the memory behind a caller's `Idempotency-Key`: the write it
	// made, and the answer it was given. Nil remembers nothing, and every write
	// then runs as it did before the header existed — the same shape as a
	// composition with no WriteLimiter, and for the same reason: a deployment that
	// mounts no keyed route, and a test that mounts one route, are not testing a
	// memory.
	//
	// It is the store rather than a middleware because the promise is about the
	// next request and often the next process: a retry arrives at whichever replica
	// the load balancer chose, after a deploy that emptied every map in the fleet.
	// kit/idempotency is that store; what this package does is decide which requests
	// are keyed and what each disposition answers with.
	Idempotency idempotency.Store
	// Operations enables durable admission for every unsafe route. Nil retains
	// the lightweight standalone router; kit/app always supplies the SQL owner.
	Operations idempotency.Operations
	// Clock supplies UTC operation instants; nil resolves to db.Now.
	Clock func() time.Time

	// Access is the sequel to a refusal: who to tell when a person asks for the
	// grant they were refused, and how. Nil draws no ask control on the refusal
	// page and mounts no ask route — the reach is another module's rows, so a
	// composition that wires none offers no door rather than one that 404s.
	Access AskForAccess

	// Accessed is the kernel's record of one ask, in the request's own transaction
	// beside the notices it wrote: kit/app publishes the event, which is where the
	// payload types and the module catalogue live. Its failure is the ask's failure —
	// an event that did not commit rolls the notices back with it — so a composition
	// that cannot write the trail refuses the ask rather than answering 202.
	Accessed func(context.Context, AccessRecord) error

	// Cache is where a resolved host is believed, and it is required: a belief this
	// package holds about which tenant a host is, and that it can be told to stop
	// holding (modules/tenant does exactly that when it suspends a customer), is not
	// this process's own business. The in-process store is a complete answer for one
	// process and the same adapter every test runs on — kit/app builds it when the
	// deployment names no server — but it has to be named, because a shared value
	// that silently became a map is a suspension that stops reaching the other
	// replicas and nobody notices until a suspended customer is still serving.
	Cache cache.Cache
}

// WriteLimiter counts one attempt and returns its decision, remaining quota and
// window from that same operation. The consumer owns this port so rendering does
// not import a storage provider; composition supplies the counter.
type WriteLimiter interface {
	// Reserve records one event under key and reports, from that one statement,
	// whether the event is within limit (the limit-th is allowed and the one after it
	// is not), how much of limit is left in the window it landed in, and how long that
	// window has left. The last two are reported on an allowed event as well as a
	// refused one: the caller who is still inside the quota is the one a stated number
	// can keep from ever arriving at the refusal.
	Reserve(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, remaining int, retryAfter time.Duration, err error)
}

// The two body ceilings, and the reason there are two.
//
// A request body with a schema is huma's business: it decodes one and it bounds
// one, at op.MaxBodyBytes, which is a megabyte unless a route says otherwise.
// A route that reads the request itself has no such bound, and there is exactly
// one of those — the file upload, whose body is read off the connection rather
// than decoded, so a caller that sends slowly is stopped by nothing at all.
//
// So every request gets an http.MaxBytesReader before it reaches a handler: a
// megabyte, or MaxUpload plus an envelope for the route that streams. The
// envelope is a megabyte because a multipart form's part headers and boundaries
// are measured in hundreds of bytes and a round number nobody has to compute is
// worth more here than a tight one.
const (
	MaxBodyBytes      = 1 << 20
	multipartEnvelope = 1 << 20
)

// StreamedBodyExtension marks an operation that reads the request body itself
// instead of declaring a schema for it. Such a route gets Options.MaxUpload
// plus an envelope rather than MaxBodyBytes; nothing else about it changes.
//
// It is an extension on the operation and not a field on Options because the
// kernel must not know which module happens to own the route. See StreamedBody.
const StreamedBodyExtension = "x-platformkit-streams-body"

// StreamedBody is what a route that reads its own request puts in its
// Extensions, beside EventsExtension. There is one caller, and there should
// stay one: a second route that reads its own body is a second route with no
// schema, no generated client and no bound but this one.
func StreamedBody() (string, any) { return StreamedBodyExtension, true }
