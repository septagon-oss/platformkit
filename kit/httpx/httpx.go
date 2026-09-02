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
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

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

// Options are the collaborators main chooses for the HTTP layer. Every field
// except Log, PublicHost and Docs is required: an API missing one of them could
// only fail closed on every request, which is worse than failing at New.
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
	Conn *db.Conn

	// Authorize answers the permission questions the declarations ask.
	Authorize Authorizer

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

	// Log receives the reason behind every denial and every rolled-back
	// transaction. Defaults to slog.Default().
	Log *slog.Logger
}

// API is the application's Huma API: the huma.API every registration goes
// through, plus the recording that makes boot-time validation possible.
type API struct {
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
	hosts  *hostCache
	token  tenancy.SystemToken
	lazy   tenancy.SystemToken
	system tenancy.SystemToken
	// resolving collapses concurrent lookups of one host into one query, so a
	// cold cache under load is one round trip and not one per request.
	resolving singleflight.Group

	mu       sync.Mutex
	ops      []*huma.Operation
	declared []tenancy.Grant
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
	case cfg.Conn == nil:
		panic("httpx.New: Options.Conn is required; every tenant request runs in a transaction")
	case cfg.Authorize == nil:
		panic("httpx.New: Options.Authorize is required; Permission declarations have nothing to ask otherwise")
	case cfg.Authenticate == nil:
		panic("httpx.New: Options.Authenticate is required; SignedIn declarations have nobody to recognise otherwise")
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
		hosts:  &hostCache{hosts: map[string]hostEntry{}},
		token:  syscap.NewSystemToken("tenant resolution"),
		lazy:   syscap.NewSystemToken("request transaction"),
		system: syscap.NewSystemToken("a module's control-plane routes"),
	}
	if a.log == nil {
		a.log = slog.Default()
	}

	// The net/http half of the chain, in order, and before any route: chi
	// refuses a middleware added after the first one is mounted, and the huma
	// adapter below mounts huma's own.
	//
	// The request id goes first, because everything below logs it and every
	// problem body carries it. respond is second, holding the response and
	// catching a panic, outside the transaction on purpose. csrf is third: a
	// cross-site write is refused before it reaches a router, a tenant or a
	// transaction. carry is last and does nothing but put the request itself on
	// the context, for the authentication hook further down, which runs inside
	// the tenant transaction and still has to read the caller's cookies.
	inner.Use(a.requestID, a.respond, a.csrf, a.carry)

	// huma.NewAPI mounts its own documentation routes through the adapter it is
	// handed, so the recorder declares those Public as they arrive: they serve
	// no tenant data and have no module to declare them, and Recorded is only
	// worth reading if it is the whole list.
	rec := &recordingAdapter{Adapter: humachi.NewAdapter(inner), api: a, builtin: true}
	a.api = huma.NewAPI(config, rec)
	rec.builtin = false
	a.adapter = rec

	// The huma half of the chain, in order. Tenant first, because everything
	// after it is scoped to one. Transaction second, so that authentication and
	// authorization can read the tenant's own rows: both are queries, and they
	// belong inside the same transaction as the work they guard. Authentication
	// third, because a session is a row of the tenant that has just resolved.
	// Authorization last, so a denial rolls that transaction back untouched.
	a.api.UseMiddleware(a.tenant, a.transaction, a.authenticate, a.authorize)

	// The API is mounted last and at the root, so a static tree registered
	// afterwards still takes precedence over it for its own prefix.
	root.Mount("/", inner)
	return a, root
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

// Static mounts a file tree beside the API, on the router that carries neither
// the request middleware nor the transaction. Static assets are not operations:
// there is no handler to authorize, no tenant transaction to open and nothing
// to declare, so they never appear in Recorded and never hold a response in
// memory waiting for a commit.
func (a *API) Static(prefix string, fsys fs.FS) {
	at := strings.TrimSuffix(prefix, "/")
	a.root.Handle(at+"/*", http.StripPrefix(at, http.FileServerFS(fsys)))
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
	var bad []string
	for _, op := range a.Recorded() {
		if _, ok := declarationOf(op); !ok {
			bad = append(bad, describe(op))
		}
	}
	if len(bad) == 0 {
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

func describe(op *huma.Operation) string {
	if op.OperationID == "" {
		return op.Method + " " + op.Path
	}
	return op.Method + " " + op.Path + " (" + op.OperationID + ")"
}
