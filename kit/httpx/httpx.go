// Package httpx builds the single Huma API the application serves, records
// every operation the adapter mounts, and enforces that each one declares
// exactly one authorization: a permission, public, or any signed-in user.
//
// # The declaration
//
// Register is the only way a module mounts a handler, and it takes an Auth
// alongside the operation, so "I forgot to say who may call this" is not a
// state a route can be in. The declaration is written into op.Extensions, which
// is the same object the OpenAPI document renders and ctx.Operation() returns at
// request time: the reviewer, the document and the middleware read one value.
//
// # Recording
//
// huma adds an operation to the OpenAPI document only when it is not hidden, so
// walking the document would miss exactly the routes most likely to be
// forgotten. Everything, hidden or not, passes through the adapter's Handle, so
// this package records there instead — huma's own documentation routes
// included, declared Public where they are mounted. ValidateDeclarations reads
// that recording and kit/app refuses to start when it reports anything.
//
// The huma.API and the adapter are both unexported fields, and neither has an
// accessor. That is not tidiness: a handler mounted straight on the adapter is
// recorded, so it passes the boot gate, and yet it is mounted below this
// package's middleware, so it resolves no tenant, opens no transaction and is
// never authorized. A door that satisfies the gate and skips the enforcement is
// worse than no gate, so there is no door. Everything mounts through Register.
//
// # The transaction
//
// This is the one place a request obtains a tenant transaction. The middleware
// chain resolves the tenant from the request host and puts a pending
// transaction on the context; TxFrom opens it on the first query, so a request
// that never touches the database never opens one. A response of 400 or worse
// rolls it back, and the response itself is held until the commit succeeds.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
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

// WriteLimiter is the counting this kernel asks for and nothing more: it is the
// shape one method wide, declared by the consumer rather than imported from the
// provider, so that a page renderer that reaches for the router does not
// inherit a dependency on how the counting is stored. kit/limit's Limiter
// satisfies it — a Postgres counter or an in-memory one, at the composition's
// choice — and a test satisfies it with a counter.
type WriteLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)
}

// The two body ceilings, and the reason there are two.
//
// A request body with a schema is huma's business: it decodes one and it bounds
// one, at op.MaxBodyBytes, which is a megabyte unless a route says otherwise.
// A route that reads the request itself has no such bound, and there is exactly
// one of those — the file upload. A review trickled a body into it at a byte a
// second and nothing anywhere said stop.
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

// API is the application's Huma API: the huma.API every registration goes
// through, plus the recording that makes boot-time validation possible.
type API struct {
	// moved is this composition's own migration rows (API.Alias), asked after the
	// kernel's table in aliases.go.
	moved []struct{ from, to string }
	// api is the recording huma.API. It is unexported because an exported one
	// is a door: huma.Register(api.API, ...) used to reach the bare adapter,
	// which the recording — and therefore the boot gate — never saw.
	api     huma.API
	adapter huma.Adapter
	// root carries the static trees; inner carries the API and the middleware
	// chain. A file has no tenant and no transaction, so it does not pay for
	// one, and a large one is not buffered waiting for a commit that will
	// never happen.
	root   *chi.Mux
	inner  *chi.Mux
	opts   Options
	log    *slog.Logger
	token  tenancy.SystemToken
	lazy   tenancy.SystemToken
	system tenancy.SystemToken
	// resolving collapses concurrent lookups of one host into one query, so a
	// cold cache under load is one round trip and not one per request.
	resolving singleflight.Group

	// access is the ask-for-access door, built once from Options and put on every
	// request's context by carry. Nil means this composition offers no way to ask.
	access *accessDoor

	mu       sync.Mutex
	ops      []*huma.Operation
	declared []tenancy.Grant
	// mounts is every route this composition mounted, with the surface it was
	// mounted on; refusals is every mount-time refusal collected on the way;
	// homes records which module took which surface's root; known is the set of
	// module names the composition registered, which is what bounds
	// Router.ForModule. All four are written only while Routes runs, which is
	// before anything serves, and read under the mutex beside the operations.
	mounts   []mounted
	refusals []string
	homes    map[Surface]string
	known    map[string]bool
	// opsHost is Options.Installation normalised once, so the gate that runs on
	// every request compares two already-normalised strings.
	opsHost string
	// resources are the entities kit/rest has mounted, for the screens that
	// are generated from them rather than written. See schemas.go.
	resources []Resource
	// commands are the lifecycle routes on those entities, by "module/entity",
	// kept beside them because the two are registered separately. See AddCommand.
	commands map[string][]Command
}

// errorShape guards the one package-global huma reads per request.
var errorShape sync.Once

// New builds the API and the router it is mounted on. The router is returned
// rather than hidden because static assets and the server itself need it; every
// route that is an API operation goes through Register.
func New(cfg Options) (*API, *chi.Mux) {
	switch {
	case cfg.Tenants == nil:
		panic("httpx.New: Options.Tenants is required; every request resolves a tenant from its host")
	case cfg.Conn == nil && !cfg.Unwired:
		panic("httpx.New: Options.Conn is required, or Options.Unwired and a call to Connect; every tenant request runs in a transaction")
	case cfg.Authorize == nil:
		panic("httpx.New: Options.Authorize is required; Permission declarations have nothing to ask otherwise")
	case cfg.Authenticate == nil:
		panic("httpx.New: Options.Authenticate is required; SignedIn declarations have nobody to recognise otherwise")
	case cfg.Cache == nil:
		panic("httpx.New: Options.Cache is required; a host resolution cached here must be one every replica can forget")
	}

	// One error shape for the whole API, assigned where the API is built rather
	// than in an init(), so the wire is visible. huma reads the global on every
	// request, so a second API built while a first one serves must not write it
	// again. See kit/problem.
	errorShape.Do(func() { huma.NewError = problem.HumaError })

	config := huma.DefaultConfig("PlatformKit", "1.0.0")
	if cfg.PublicHost != "" {
		config.Servers = []*huma.Server{{URL: "https://" + cfg.PublicHost}}
	}
	if !cfg.Docs {
		config.OpenAPIPath = ""
		config.DocsPath = ""
	}
	config.Transformers = append(config.Transformers, stampRequestID)

	root, inner := chi.NewMux(), chi.NewMux()
	a := &API{
		root:   root,
		inner:  inner,
		opts:   cfg,
		log:    cfg.Log,
		token:  syscap.NewSystemToken("tenant resolution"),
		lazy:   syscap.NewSystemToken("request transaction"),
		system: syscap.NewSystemToken("a module's control-plane routes"),
	}
	if cfg.Access != nil {
		// One door, read per request off the context: the catalogue and the
		// limiter belong to the API, the reach belongs to the composition, and the
		// two page and JSON routes call the same command (docs/adr/0007).
		a.access = &accessDoor{
			ask: cfg.Access, limiter: cfg.WriteLimiter, record: cfg.Accessed,
			catalogue: a.Permissions,
		}
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	a.opsHost = HostOnly(cfg.Installation)

	// Which surface is this address, then the security headers that surface
	// decided, then whether the address is served here at all — outermost and on
	// the router that carries the static tree as well as the API: a stylesheet, a
	// 404 from chi and a panic that never reached a handler are all responses a
	// browser acts on. chi refuses a middleware added after the first route, so
	// all three are here rather than beside Static.
	//
	// The gate runs *inside* the headers, and that order is the whole of its
	// secrecy: the answer it gives for an address it refuses is the answer an
	// address nobody mounted gets, and to be that answer it has to be dressed by
	// the same layer that dresses every other answer of this host. A gate that
	// answered ahead of the headers was distinguishable from an unmounted address
	// by five response headers, which is the one fact it exists to hide. See
	// surfaces.go and headers.go.
	root.Use(tracing, a.requestID, a.surface, a.headers, a.address)

	// What chi itself answers when nothing matched, or matched but not for this verb.
	//
	// These are the refusals a browser runs into most often — a mistyped address, a
	// bookmark left over from an earlier deployment, a form action that outlived the
	// route it pointed at — and none of them reaches a module handler, so the answer used
	// to be net/http's plain-text "404 page not found": a note meant for a developer,
	// displayed in a browser window with no way onward. Through a.fail they are the same
	// verdict as every other refusal and take the shape the client asked for, which is
	// also what keeps a monitor seeing a problem document it can parse.
	root.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.fail(w, r, http.StatusNotFound, "nothing is served at this address")
	}))
	root.MethodNotAllowed(http.HandlerFunc(a.methodNotAllowed))

	// The net/http half of the chain, in order, and before any route: chi
	// refuses a middleware added after the first one is mounted, and the huma
	// adapter below mounts huma's own.
	//
	// respond is first, holding the response and catching a panic, outside the
	// transaction on purpose. csrf is second: a cross-site write is refused
	// before it reaches a router, a tenant or a transaction. carry is last and
	// does nothing but put the request itself on the context, for the
	// authentication hook further down, which runs inside the tenant transaction
	// and still has to read the caller's cookies.
	//
	// The request id is not here: it went to the root router with the surfaces,
	// so that the refusals chi answers for itself — an address nobody mounted —
	// carry one too. A person quoting a 404 and an operator reading the log have
	// to land on the same request, and the 404 is the response most likely to be
	// quoted.
	// idempotencyRecord sits immediately inside respond for the one reason respond
	// states for itself: that is the place that knows what the client was finally
	// given, and a stored answer must be the answer, not the one a handler meant to
	// write before the commit failed. It is outside the transaction on purpose —
	// inside it, the record of a response would be a fact a rollback could take
	// back while the caller already holds the response.
	inner.Use(a.respond, a.idempotencyRecord, a.csrf, a.carry)

	// huma.NewAPI mounts its own documentation routes through the adapter it is
	// handed, so the recorder declares those Public as they arrive: they serve
	// no tenant data and have no module to declare them, and Recorded is only
	// worth reading if it is the whole list.
	rec := &recordingAdapter{Adapter: humachi.NewAdapter(inner), api: a, builtin: true}
	a.api = huma.NewAPI(config, rec)
	rec.builtin = false
	a.adapter = rec

	// The huma half of the chain, in order. Tenant first, because everything
	// after it is scoped to one. The public write limit second, and that place is
	// the point twice over: the key it counts under names the tenant the host just
	// resolved, and it is still ahead of the transaction, so a refused anonymous
	// write is not a request that opened a transaction to be told no and the count
	// survives the refusal by being written on a detached context of its own.
	// Transaction third, so that authentication and authorization can read the
	// tenant's own rows: both are queries, and they belong inside the same
	// transaction as the work they guard. Authentication fourth, because a session
	// is a row of the tenant that has just resolved. Authorization fifth, so a
	// denial rolls that transaction back untouched. Bodies last, after every guard
	// and before the handler, which is where the two things it does both belong:
	// nothing above it reads a body, and the transaction it ends for a streaming
	// route is the one the guards opened.
	// Idempotency last, past every guard, and that place is the whole of its
	// security: the claim carries the tenant and the caller the context resolved, so
	// no header can supply either, and a caller the authorizer refused never leaves a
	// key behind. Ahead of the handler and behind the body's ceiling, because the
	// claim is written before the command runs and a claim written after it is a
	// claim a crash takes with it.
	a.api.UseMiddleware(a.tenant, a.traced, a.publicWrites, a.transaction, a.authenticate, a.authorize, a.bodies, a.idempotency)

	// The API is mounted last and at the root, so a static tree registered
	// afterwards still takes precedence over it for its own prefix.
	root.Mount("/", inner)
	return a, root
}

// Connect hands an unwired API the two things its deployment opens after its
// routes are built: the connection every request transaction opens on, and the
// store a resolved host is believed in. kit/app.Start is its only caller, and the
// reason it exists is the ordering decision 0074 rule 1 makes: which routes the
// modules mounted is the last thing a composition can get wrong for free, and the
// pool and the shared store are the first things it spends. An API therefore
// registers its routes and answers every gate over them before either is dialled,
// and the connection arrives here, after the last registration and before anything
// can listen.
//
// Nothing is re-mounted. Both values are read per request and neither while a
// route is being mounted, so the middleware chain, every operation, every page and
// every static tree is the one the gates read; the OpenAPI document is unchanged.
// The in-process store the build was handed is left for its owner to close — it
// belongs to the build, not to this API — and from this call every host resolution
// is believed in the store every replica can forget, which is the store the
// deployment named and not the one a build that might still be refused invented.
//
// A nil connection, a nil store and a second call on an API that already has a
// connection are all refused: the first two describe an API that could only fail
// closed on every request, and the third is two deployments' connections in one
// router, one of which somebody closes while the other is still serving.
func (a *API) Connect(conn *db.Conn, store cache.Cache) error {
	switch {
	case conn == nil:
		return errors.New("httpx.Connect: the connection is required; a request transaction opens on it")
	case store == nil:
		return errors.New("httpx.Connect: the store is required; a host resolution this package holds has to be one every replica can forget")
	case a.opts.Conn != nil:
		return errors.New("httpx.Connect: this API already has a connection; a second one is a second deployment in one router")
	}
	a.opts.Conn = conn
	a.opts.Cache = store
	return nil
}

// methodNotAllowed answers an address that is served but does not take this
// verb. It is chi's answer, with one exception that is not chi's: a resource
// whose reads answer at this address and whose writes answer on the surface its
// write permission belongs on — a price list, read by the tenant that pays for it
// and written by the installation — is asked at its read door by exactly the
// caller who derived that door from the catalog and derived it wrong.
//
// For them chi's sentence, "this address does not accept POST requests", is the
// one an address that never heard of the resource also says, and the catalog they
// are holding does name the door: it is the entry's write_path, which is the same
// sentence this answer gives. The write is still refused, and refused it writes
// nothing and publishes nothing — what changes is that the refusal carries the
// way out. See API.writeElsewhere for when the address is named at all.
func (a *API) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	if at, entity := a.writeElsewhere(r); at != "" {
		a.rlog(r.Context()).InfoContext(r.Context(), "httpx: a write of a split resource was asked at its read door",
			"code", CodeWriteElsewhere, "method", r.Method, "path", r.URL.Path, "writes", at, "entity", entity)
		a.fail(w, r, http.StatusForbidden, CodeWriteElsewhere+": this address reads the "+entity+" and does not write it; write it at "+at)
		return
	}
	a.fail(w, r, http.StatusMethodNotAllowed, "this address does not accept "+r.Method+" requests")
}

// SystemToken is the capability that opens a cross-tenant transaction, handed
// to a module at the one moment it is being wired: Module.Routes is given this
// API, and a module that registers a control-plane route takes the token there.
//
// It is a method rather than something kit/internal/syscap would mint for
// anybody, because the point is that the set of modules holding one is short and
// visible. `grep -rn 'SystemToken()' modules/` is that list, and there is no
// other door: nothing outside kit/ can construct or implement a token, so a
// module that wants to cross tenants has to write this call where a reviewer
// reading the manifest will see it. See docs/adr/0006.
//
// A route that holds one still runs inside its own tenant's transaction, so it
// opens the system transaction on a detached context (db.Detached) — two
// transactions, and the control-plane one commits on its own.
func (a *API) SystemToken() tenancy.SystemToken { return a.system }

// Probes mounts handlers beside the API, on the router that carries neither the
// request middleware nor a transaction. kit/health is the only caller, and the
// two paths it names are /health and /ready.
//
// A probe is not an operation. It has no tenant — an orchestrator reaches an
// instance at a pod address that names no site — no session, and nothing to
// declare. Going through the chain would make liveness depend on the one query
// every request makes before it is a request: the host lookup, which has a two
// second budget of its own and never hits the cache for a pod address, because
// only a successful resolution is cached. A liveness probe with a two second
// timeout therefore fired during a database outage and the kubelet restarted
// pods whose only problem was that their database was unreachable, which is the
// exact failure the probes exist to avoid.
//
// This is a narrow door and not a general "mount a handler" one, for the reason
// the package comment gives: a handler mounted below this package's middleware
// resolves no tenant, opens no transaction and is never authorized, so the only
// handlers that may take it are the ones that must answer without any of the
// three. Static is the other. Both are named for what they carry.
func (a *API) Probes(h http.Handler, paths ...string) {
	for _, p := range paths {
		a.root.Handle(p, h)
	}
}

// recordingAdapter is where every registration made through this API is seen:
// huma.Register and any raw handler alike end at Adapter.Handle, hidden or not.
// While builtin is set, huma is mounting its own documentation routes and the
// recorder declares them Public.
type recordingAdapter struct {
	huma.Adapter
	api     *API
	builtin bool
}

func (r *recordingAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	if r.builtin && op != nil {
		declare(op, Public())
	}
	r.api.record(op)
	r.Adapter.Handle(op, handler)
}

// record stores the pointer the adapter was handed, not a copy. That is the
// property the scheme rests on: boot validation and the request-time middleware
// read the same object, so a declaration cannot be visible to one and missing
// from the other.
func (a *API) record(op *huma.Operation) {
	if op == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ops = append(a.ops, op)
}

// Recorded returns every operation the adapter has handled, in registration
// order, hidden ones included.
func (a *API) Recorded() []*huma.Operation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ops)
}

// ValidateDeclarations names every operation that does not carry an
// authorization this package minted. kit/app calls it once every route is
// registered and refuses to serve when it returns anything; the request-time
// middleware denies the same operations, so this turns a 403 nobody notices
// into a startup failure someone has to fix.
func (a *API) ValidateDeclarations() error {
	// Huma mounts a route of its own — /schemas/{schema}, which a problem
	// document's $schema points at — and it was not mounted through a router, so
	// nothing stamped its surface. The address is the answer, and it is the same
	// function the request chain runs: an operation that reaches the document
	// without saying where it lives is stated here rather than left unstated.
	for _, op := range a.Recorded() {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		if _, ok := op.Extensions[SurfaceExtension]; !ok {
			op.Extensions[SurfaceExtension] = string(classify(op.Path))
		}
	}
	var bad, unanswerable, idempotent []string
	for _, op := range a.Recorded() {
		if why := idempotencyRefusal(op); why != "" {
			idempotent = append(idempotent, describe(op)+": "+why)
		}
		auth, ok := declarationOf(op)
		if !ok {
			bad = append(bad, describe(op))
			continue
		}
		if auth.Feature() != "" && a.opts.Entitle == nil {
			unanswerable = append(unanswerable, describe(op)+" needs the "+auth.Feature()+" feature")
		}
	}
	if len(idempotent) > 0 {
		sort.Strings(idempotent)
		return fmt.Errorf("httpx: %d operation(s) declare %s and cannot mean it:\n  %s",
			len(idempotent), IdempotencyKeyHeader, strings.Join(idempotent, "\n  "))
	}
	if len(unanswerable) > 0 {
		sort.Strings(unanswerable)
		return fmt.Errorf("httpx: %d operation(s) declare a plan feature and Options.Entitle is nil:\n  %s",
			len(unanswerable), strings.Join(unanswerable, "\n  "))
	}
	if len(bad) == 0 {
		// The surface gate runs last and only when nothing else failed, because
		// a route that was never declared has no surface either and the two
		// messages together would read as two faults where one was made.
		if refusals := a.validateSurfaces(); len(refusals) > 0 {
			return fmt.Errorf("httpx: %d route(s) contradict the router they are mounted on:\n  %s",
				len(refusals), strings.Join(refusals, "\n  "))
		}
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("httpx: %d operation(s) do not declare their authorization; register them with httpx.Register:\n  %s",
		len(bad), strings.Join(bad, "\n  "))
}

// Required lists, once each, every grant a recorded operation asks for, with
// the operator flag the route declared. kit/app checks it against the
// manifests, so a permission nobody defines — or one the two sides disagree
// about — fails startup rather than denying everyone forever or, worse, letting
// a customer's wildcard through the control plane.
func (a *API) Required() []tenancy.Grant {
	seen := map[tenancy.Grant]bool{}
	var out []tenancy.Grant
	for _, op := range a.Recorded() {
		auth, ok := declarationOf(op)
		if !ok {
			continue
		}
		g, asks := auth.grant()
		if !asks || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Permission < out[j].Permission })
	return out
}

// Declare records every permission the composition defines. kit/app calls it
// once, from the manifests, before any module registers its routes.
//
// It is here rather than in a module's Deps because the one module that needs
// the list — auth, which refuses a role naming a permission nobody defines —
// would otherwise be handed what every other module declares, and a module that
// knows the catalogue knows its neighbours. Module.Routes is given this API,
// which is the one moment the kernel has the whole list.
func (a *API) Declare(grants []tenancy.Grant) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.declared = slices.Clone(grants)
	sort.Slice(a.declared, func(i, j int) bool { return a.declared[i].Permission < a.declared[j].Permission })
}

// Permissions is every permission the composition defines, in name order. A
// module validating a list somebody typed asks this, not its neighbours.
func (a *API) Permissions() []tenancy.Grant {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.declared)
}

// EventsExtension is the OpenAPI extension an operation lists the events its
// handler publishes under. kit/rest writes it when it mounts a Spec, and
// kit/app reads it back to check that some module declared each one — the same
// recording, the same object and the same gate as the authorization
// declaration, rather than a second channel to keep in step.
const EventsExtension = "x-platformkit-events"

// Events lists, once each, every event a recorded operation says it publishes.
func (a *API) Events() []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range a.Recorded() {
		if op.Extensions == nil {
			continue
		}
		names, _ := op.Extensions[EventsExtension].([]string)
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// idempotencyRefusal names the reason a declaration of the idempotency key cannot
// mean what it says, or "" when it can. Three shapes, each a mistake about what
// the header is:
//
//   - A safe method: a key claims "run this once", and a GET is a read the kernel
//     already answers from whatever the row says now.
//   - The Public surface: it has no principal, so the key would be scoped to the
//     whole tenant — a table any anonymous visitor can fill with keys and any
//     other visitor of that tenant could then replay. An anonymous double submit is
//     the public write limit's question, and this does not answer it.
//   - A streamed body: the gate hashes the bytes, and the one route that reads its
//     own has consumed them by the time any gate could look.
func idempotencyRefusal(op *huma.Operation) string {
	if !declaresIdempotency(op) {
		return ""
	}
	switch {
	case !unsafeMethod(op.Method):
		return "a " + op.Method + " is a read, which has no outcome to make happen once"
	case op.Extensions[StreamedBodyExtension] != nil:
		return "the handler reads its own body, which leaves nothing for the request gate to hash"
	}
	s, _ := op.Extensions[SurfaceExtension].(string)
	if Surface(s) == SurfacePublic {
		return "the public surface has no principal to scope a key to, so any visitor of the tenant could replay another's"
	}
	return ""
}

func describe(op *huma.Operation) string {
	if op.OperationID == "" {
		return op.Method + " " + op.Path
	}
	return op.Method + " " + op.Path + " (" + op.OperationID + ")"
}
