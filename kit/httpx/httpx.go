// Package httpx builds the Huma API and records every declared operation, including
// hidden and builtin routes. Register supplies authorization metadata; the boot
// gate validates it before serving. Tenant requests share one lazy transaction
// and buffer responses until finalisation. See README.md for surface and lifetime
// contracts; the raw adapter stays private so callers cannot bypass them.
//
// The [Register] example mounts a typed operation with explicit authorization.
// It inspects the registered route without opening a server or serving tenant work.
package httpx

import (
	"errors"
	"fmt"
	"github.com/septagon-oss/platformkit/kit/screen"
	"log/slog"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// API is the application's Huma API: the huma.API every registration goes
// through, plus the recording that makes boot-time validation possible.
type API struct {
	screenViews []screen.View
	// integers is every operation registered with its own integer view of the
	// document, so the gate can render each negotiated form. See int64_api.go.
	integers []*integerAPI
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
	// addresses are the protocol addresses modules declared with Router.At, and
	// frozen says the mount gate has read the table. Both are written only while
	// Routes runs and read under the same mutex beside them. The freeze is what
	// makes the answer validateSurfaces checked every route against the answer
	// the middleware gives the request: nothing declares an address after the
	// gate has read the table, so the two cannot hold different opinions.
	addresses []addressClaim
	frozen    bool
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
	cfg = requiredOptions(cfg)

	// One error shape for the whole API, assigned where the API is built rather
	// than in an init(), so the wire is visible. huma reads the global on every
	// request, so a second API built while a first one serves must not write it
	// again. See kit/problem.
	errorShape.Do(func() { huma.NewError = problem.HumaError })

	config, badMediaTypes := documentConfig(cfg)

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
	// A media type the composition cannot name is a refusal, not a guess: an
	// entry of the wrong shape registers nothing, and a protocol whose clients
	// send the type the composition meant would meet a 415 nobody can explain.
	for _, mt := range badMediaTypes {
		a.noteRefusal(fmt.Sprintf("httpx: Options.JSONMediaTypes holds %q; a media type is type/subtype or type/subtype+suffix, in lower case", mt))
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

	// Address refusals and static responses share the same outer headers.
	root.Use(tracing, a.requestID, a.surface, a.headers, a.address)

	// Router refusals use the same negotiated error shape as handlers.
	root.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.fail(w, r, http.StatusNotFound, "nothing is served at this address")
	}))
	root.MethodNotAllowed(http.HandlerFunc(a.methodNotAllowed))

	// Buffer outside the transaction; reject CSRF before resolving the tenant.
	inner.Use(a.respond, a.csrf, a.carry)

	// huma.NewAPI mounts its own documentation routes through the adapter it is
	// handed, so the recorder declares those Public as they arrive: they serve
	// no tenant data and have no module to declare them, and Recorded is only
	// worth reading if it is the whole list.
	rec := &recordingAdapter{Adapter: humachi.NewAdapter(inner), api: a, builtin: true}
	a.api = huma.NewAPI(config, rec)
	rec.builtin = false
	a.adapter = rec

	// Resolve the tenant before its transaction and grants. Admission reads the
	// bounded body after authorization; respond records only the final answer.
	a.api.UseMiddleware(a.tenant, a.traced, a.publicWrites, a.transaction, a.authenticate, a.authorize, a.writeLimits, a.bodies, a.idempotency)

	// The API is mounted last and at the root, so a static tree registered
	// afterwards still takes precedence over it for its own prefix.
	root.Mount("/", inner)
	return a, root
}

// Connect attaches an unwired API's connection and shared host cache after route
// validation and before serving. Nil inputs or an already attached connection
// are refused. Routes stay mounted, and the caller retains ownership of the old
// cache. See README.md for the database boundary.
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

// Cache is the installation's one shared store — the same adapter host
// resolution is believed in, and the same one every process that shares this
// deployment reads.
//
// It is reachable because a composition that owns a route has to be able to
// spread a bounded read across the processes, and building a second store would
// mean the invalidations this package makes never reached it. The caller re-asks
// its own authorization before consulting the store: a value is never handed out
// on the strength of the key that found it, because nothing that forms a key has
// decided anything about the caller.
func (a *API) Cache() cache.Cache { return a.opts.Cache }

// Probes mounts liveness/readiness handlers outside tenant resolution,
// authentication and transactions so database outages cannot turn liveness into
// a restart loop. Only handlers requiring none of those guards belong here.
func (a *API) Probes(h http.Handler, paths ...string) {
	for _, p := range paths {
		a.root.Handle(p, h)
	}
}

// schemaNamer is huma's own namer with one concession, and the concession is
// the language overlay's: a translated page's body *is* the page's body — the
// same items, the same paging, the same total — and the overlay adds a header
// and answers a query parameter, not a member. Left alone, huma names the
// anonymous struct behind a translated page after its parent, so the same
// document shape would be published twice under "PageContentBody" and
// "TranslatedPageContentBody": a client generator would emit two types for one
// wire object, and this repository's own OpenAPI gate (rule B3) reads a renamed
// schema where no member moved. Only unnamed struct hints are rewritten: they
// are generated from the parent's name and are the overlay's whole footprint,
// while named types — a TranslateBody that genuinely is a new shape — keep
// whatever name the type itself states.
func schemaNamer(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	if t.Name() == "" {
		name = strings.TrimPrefix(name, "Translated")
	}
	return name
}

func requiredOptions(cfg Options) Options {
	if cfg.Clock == nil {
		cfg.Clock = db.Now
	}
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

	return cfg
}

func documentConfig(cfg Options) (huma.Config, []string) {
	config := huma.DefaultConfig("PlatformKit", "1.0.0")
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", schemaNamer)
	// Every JSON form this composition serves decodes through one codec, so a
	// body keeps its int64 digits whichever media type carried it. The private
	// int64 media renders decimal strings instead of numbers.
	config.Formats = maps.Clone(config.Formats)
	format := huma.DefaultJSONFormat
	format.Unmarshal = jsonbody.Decode
	config.Formats["application/json"], config.Formats["json"] = format, format
	config.Formats[int64JSONMediaType] = huma.Format{Marshal: jsonbody.Encode, Unmarshal: jsonbody.Decode}
	// The extra JSON media types go into the same map the defaults went into,
	// bound to the same format: parsing and rendering stay JSON's, and the type
	// name is what a protocol client negotiated for.
	var badMediaTypes []string
	for _, mt := range cfg.JSONMediaTypes {
		if !validJSONMediaType(mt) {
			badMediaTypes = append(badMediaTypes, mt)
			continue
		}
		if mt != int64JSONMediaType {
			config.Formats[mt] = huma.DefaultJSONFormat
		}
	}
	if cfg.PublicHost != "" {
		config.Servers = []*huma.Server{{URL: "https://" + cfg.PublicHost}}
	}
	if !cfg.Docs {
		config.OpenAPIPath = ""
		config.DocsPath = ""
	}
	config.Transformers = append(config.Transformers, stampRequestID)

	return config, badMediaTypes
}
