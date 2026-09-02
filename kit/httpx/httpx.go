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
// this package records there instead. ValidateDeclarations reads that recording
// and kit/app refuses to start when it reports anything.
//
// # The transaction
//
// This is the one place a request obtains a tenant transaction. The middleware
// chain resolves the tenant from the request host, opens db.Run around the rest
// of the request, and puts the resulting db.Tx[db.Tenant] where TxFrom finds it.
// A handler or repository never opens its own; a response of 400 or worse rolls
// the transaction back.
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

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Authorizer decides whether the caller of a request may exercise a permission
// in a tenant. The auth module implements it; a test implements it in three
// lines. It is the only thing this package knows about roles.
type Authorizer interface {
	Allowed(ctx context.Context, tenant tenancy.Tenant, permission string) (bool, error)
}

// Options are the collaborators main chooses for the HTTP layer. Every field
// except Log and PublicHost is required: an API missing one of them could only
// fail closed on every request, which is worse than failing at New.
type Options struct {
	// PublicHost is the host the application believes it is reached at. It
	// names the server in the OpenAPI document; the tenant of a request always
	// comes from that request's own Host header, never from this.
	PublicHost string

	// Tenants maps a request host to a tenant.
	Tenants tenancy.Resolver

	// Conn is the application connection every request transaction opens on.
	Conn *db.Conn

	// Authorize answers the permission questions the declarations ask.
	Authorize Authorizer

	// Authenticate reads the caller's principal off the raw request, before
	// routing. It reports false for an anonymous caller, which is not an error:
	// a Public operation serves them. Sessions replace the hook in E3.
	Authenticate func(r *http.Request) (Principal, bool)

	// Log receives the reason behind every denial and every rolled-back
	// transaction. Defaults to slog.Default().
	Log *slog.Logger
}

// API is the application's Huma API: the huma.API every registration goes
// through, plus the recording that makes boot-time validation possible.
type API struct {
	huma.API

	adapter huma.Adapter
	router  *chi.Mux
	opts    Options
	log     *slog.Logger

	mu  sync.Mutex
	ops []*huma.Operation
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

	router := chi.NewMux()
	a := &API{router: router, opts: cfg, log: cfg.Log}
	if a.log == nil {
		a.log = slog.Default()
	}

	// Authentication is an ordinary net/http middleware because its hook reads
	// an *http.Request, and because a principal is a property of the caller
	// rather than of the route: it is established once, before routing, and the
	// authorization middleware below decides what it is worth. chi refuses a
	// middleware added after the first route, and humachi.New mounts routes, so
	// this goes first.
	router.Use(a.authenticate)

	// humachi.New mounts huma's own /openapi.*, /docs and /schemas routes on the
	// adapter it is handed. Wrapping the recorder around the adapter afterwards
	// keeps those six out of the recording, which is deliberate: they are the
	// API's documentation, they read no tenant data, and they are not
	// operations any module declared. Everything registered from here on is
	// recorded and must declare.
	inner := humachi.New(router, config)
	a.API = inner
	a.adapter = recordingAdapter{Adapter: inner.Adapter(), api: a}

	// The chain, in order. Tenant first, because everything after it is scoped
	// to one. Transaction second, so that authorization can read the tenant's
	// own rows: a permission check in E3 is a query, and it belongs inside the
	// same transaction as the work it guards. Authorization last, so a denial
	// rolls that transaction back untouched.
	a.UseMiddleware(a.tenant, a.transaction, a.authorize)
	return a, router
}

// Adapter returns the recording adapter, so every operation registered on this
// API is seen by Recorded even when it is hidden from the OpenAPI document.
func (a *API) Adapter() huma.Adapter { return a.adapter }

// Config keeps huma's unexported configProvider assertion working through the
// wrapper: huma.Group and huma.Register read the configuration this way, and a
// wrapper that could not answer would hand them a zero Config.
func (a *API) Config() huma.Config {
	return a.API.(interface{ Config() huma.Config }).Config()
}

// Static mounts a file tree on the router, outside the API. Static assets are
// not operations: there is no handler to authorize, no tenant transaction to
// open and nothing to declare, so they never appear in Recorded.
func (a *API) Static(prefix string, fsys fs.FS) {
	a.router.Handle(strings.TrimSuffix(prefix, "/")+"/*",
		http.StripPrefix(strings.TrimSuffix(prefix, "/"), http.FileServerFS(fsys)))
}

// recordingAdapter is where every registration made through this API is seen:
// huma.Register and any raw handler alike end at Adapter.Handle, hidden or not.
// Reaching past the embedded huma.API — huma.Register(api.API, ...) — registers
// on the bare adapter and is invisible to the recording and therefore to the
// boot gate. That is a deliberate escape hatch of Go's embedding, not a hole the
// gate can close; the request-time middleware still denies such an operation.
type recordingAdapter struct {
	huma.Adapter
	api *API
}

func (r recordingAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
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
		if _, ok := declarationOf(op); ok {
			continue
		}
		reason := "no declaration"
		if op.Extensions != nil {
			if v, present := op.Extensions[AuthExtension]; present {
				reason = "the declaration is not an httpx.Auth"
				if _, isAuth := v.(Auth); isAuth {
					reason = "the declaration is the zero httpx.Auth, which no constructor returns"
				}
			}
		}
		bad = append(bad, describe(op)+": "+reason)
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("httpx: %d operation(s) do not declare their authorization; register them with httpx.Register:\n  %s",
		len(bad), strings.Join(bad, "\n  "))
}

// PublicMutations returns "METHOD /path" for every recorded operation that
// declares itself public and changes state. It is the surface an
// unauthenticated caller can write through, so it is the list a reviewer reads
// first. E2 pins it against the reference application's composition, and from
// then on a newly public write is a failing diff rather than a discovery.
func (a *API) PublicMutations() []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range a.Recorded() {
		if !mutates(op.Method) {
			continue
		}
		auth, ok := declarationOf(op)
		if !ok || auth.kind != kindPublic {
			continue
		}
		route := strings.ToUpper(op.Method) + " " + op.Path
		if seen[route] {
			continue
		}
		seen[route] = true
		out = append(out, route)
	}
	sort.Strings(out)
	return out
}

func mutates(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func describe(op *huma.Operation) string {
	if op.OperationID == "" {
		return op.Method + " " + op.Path
	}
	return op.Method + " " + op.Path + " (" + op.OperationID + ")"
}
