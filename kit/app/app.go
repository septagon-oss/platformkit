// Package app assembles a running application from a configuration and a list
// of modules.
//
// New checks the composition; Run migrates, opens the connection, and then does
// whichever half of the work this process's role names: web builds the API,
// proves that every operation declared its authorization and every event it
// publishes was promised, and serves; worker relays the outbox, consumes
// subscriptions, schedules the periodic jobs and answers the two probes; all
// does both in one process. The order is written here, once, in the order it
// happens — which is the whole argument of docs/adr/0002: a startup sequence
// that is read rather than derived. Run owns the listener it serves on; Start
// hands the same sequence, up to the point of listening, to an application that
// owns a listener of its own.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Role is which half of the application a process runs. One binary and one
// image; the role is a flag. See docs/adr/0005.
type Role string

const (
	// Web serves the API and nothing else.
	Web Role = "web"
	// Worker relays the outbox, consumes events and runs the periodic jobs.
	Worker Role = "worker"
	// All is both in one process, which is what a laptop and a small
	// deployment want. It is the default, so a deployment that says nothing
	// gets a whole application.
	All Role = "all"
)

// Options are the cross-cutting implementations main chooses: the three
// questions the kernel cannot answer for itself — which host is which tenant,
// who is calling, and what they may do — plus the role, the event transport
// constructors, the tenant list the periodic jobs walk, and where to log.
type Options struct {
	// Clock supplies operation admission, completion and retention instants.
	// Nil uses db.Now; constructors resolve it before composing routes.
	Clock     func() time.Time
	Tenants   httpx.TenantLoader
	Authorize httpx.Authorizer
	// Entitle answers what a tenant's plan includes, for the operations whose
	// declaration names a feature. It is optional: an application that sells
	// nothing declares no feature and is never asked. One that does declare a
	// feature and leaves this nil fails to start, which is httpx's check and
	// not this one.
	Entitle      httpx.Entitler
	Authenticate func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)
	Log          *slog.Logger

	// Fault renders a kernel refusal as a document, for the client that came to be
	// looked at rather than to read a value. page.FaultHandler is the implementation for
	// an application with a shell; nil keeps every kernel refusal as problem+json, which
	// is what an application without a shell wants and what every application did before
	// the field existed.
	//
	// It is supplied here rather than composed inside kit/app because a failure page is
	// chrome — brand, stylesheet, where "back" goes — and chrome is ui, which kit may not
	// import. The application that owns the look owns the failure page too.
	Fault httpx.Fault

	// Role defaults to All.
	Role Role

	// App names this composition in broker subjects, durables, locks and cookies.
	// Empty preserves the single-application names. New reconciles it with nats.app
	// in its configuration copy and refuses conflicting or malformed names.
	App appname.Name

	// Installation names the host the installation itself is reached at — the
	// one address that serves the control plane (the Ops surface) and the only
	// place an operator's own console answers.
	//
	// It is a host and not a tenant: no TenantLoader is asked about it, because
	// the address names the installation rather than a customer. Left empty, the
	// control plane mounts and every request to it is answered as an address
	// nothing serves, and boot says so: a control plane nobody can reach is a
	// decision, and a silent one is a surprise during the incident it was for.
	Installation Installation

	// JSONMediaTypes are the media types this installation reads and writes as
	// JSON, beside application/json. A protocol that names its own type arrives
	// with a Content-Type the API has never been told about, and refuses to parse
	// anything carrying it, so which types are JSON is a fact about what the
	// installation serves and belongs with the rest of those facts.
	//
	// httpx.Options.JSONMediaTypes is where it is spent; see that field for why a
	// module cannot say it itself.
	JSONMediaTypes []string

	// WorkspaceCatalog mounts the catalog route the kernel owns the address of,
	// /api/v1/app/resources: the resources this caller may reach, as a document.
	// Build it with WorkspaceCatalogRoute; the composition writes one line.
	//
	// It is a mount and not a function returning the body because the body's type
	// has to reach the OpenAPI document, and a func returning `any` erases it
	// before anything can read it. It stays a field rather than a call the
	// composition makes because the two owners are still different: only the kernel
	// can compose the address (no module composes /api/v1/app/…, and the address is
	// a native shell's contract, not a capability's), and only ui can render the
	// answer — and kit may not import ui (scripts/check_packages.sh is that line).
	// The composition names its renderer here, one literal, and a composition with
	// resources and no renderer is refused: a native shell would have nothing to
	// read.
	WorkspaceCatalog func(api *httpx.API)

	// Access is the reach an ask for access needs: the people in this tenant
	// whose role grants role management, and the notice written to each. Nil
	// leaves the kernel's ask door unmounted and the refusal page with no ask
	// control, which is the honest answer — see httpx.Options.Access.
	Access httpx.AskForAccess

	// AccessPage mounts the browser's half of the same command (the form the
	// refusal page posts to, and the confirmation page). The address is the
	// kernel's, the body is the composition's, because kit may not import ui:
	// ui/page.MountAccess is what a composition passes here. It is called with
	// the workspace router and only when Access is wired.
	AccessPage func(router *httpx.Router)

	// Transport carries events between the relay and the handlers. An explicit
	// value overrides nats.transport and Transports. Otherwise All defaults to
	// memory and a separate Worker to JetStream; nats.transport can opt All
	// into JetStream, and Transports says how each is built.
	Transport events.Transport

	// Transports are the constructors behind nats.transport's two names. New
	// refuses a composition whose selected name has no constructor, so a
	// worker that would have found out at Run finds out before anything is
	// opened; the check runs in every role, because one image must answer
	// "will this start?" the same way whichever half it runs.
	Transports Transports

	// Caches is the constructor behind cache.adapter's one shared name. The
	// kernel names the store an entry every replica must read lives in, and builds
	// none of them: an installation that sets cache.adapter to valkey is refused at
	// New if this composition never named how to reach one. An empty adapter is the
	// in-process store, which needs no constructor because it opens nothing — and
	// says so once, in a boot line, because a cache that reaches one process is a
	// deployment choice an operator should not discover from a stale host.
	Caches Caches
}

// Caches is how the stores cache.adapter can name are reached. The kernel knows
// the names and the rule between them; the composing application is the one
// importer of the provider package, so kit/app does not link a cache client any
// more than it links a broker (scripts/check_packages.sh records that), and a
// product that wants a different server supplies its own constructor here.
type Caches struct {
	// Valkey builds the shared store from the cache settings New has already
	// validated. It is called with the boot context, and a store that cannot be
	// reached within its own boot budget is a boot failure rather than a cache that
	// misses forever: a deployment that says "shared" and silently gets a private
	// one has lost every invalidation it will ever run.
	Valkey func(ctx context.Context, cfg config.Cache) (cache.Cache, error)
}

// constructor is the Caches field the selected adapter needs, or the error that
// names the missing one — Transports.constructor's shape, for the same reason: an
// option a composition forgot is a boot refusal, not a runtime surprise.
func (c Caches) constructor(adapter string) (func(context.Context, config.Cache) (cache.Cache, error), error) {
	if adapter != "valkey" {
		return nil, nil
	}
	if c.Valkey == nil {
		return nil, errors.New("app: cache.adapter selects valkey and Options.Caches.Valkey is nil; the application supplies the constructor (kit/cache/providers/valkey.Connect)")
	}
	return c.Valkey, nil
}

// Transports is how the two event transports nats.transport can name are
// built. The kernel knows the names and the rule between them — memory is one
// process talking to itself, so a separate worker cannot use it — and nothing
// about how either is made: the composing application supplies both and is
// the one importer of the provider packages, so kit/app links neither the
// NATS client nor the in-process queue (scripts/check_packages.sh records
// that). apps/platformkit passes memory.New and nats.Connect; a product that
// runs one process forever may leave JetStream nil and set nats.transport to
// memory, and is refused the day a separate worker is deployed against it.
type Transports struct {
	// Memory builds the in-process transport the combined role defaults to.
	Memory func() events.Transport
	// JetStream builds the shared broker transport a separate role defaults
	// to, from the NATS settings New has already validated.
	JetStream func(config.NATS) (events.Transport, error)
}

// constructor is the Transports field the selected mode needs, or the error
// that names the missing one. It is the whole of what New checks about
// transports beyond the mode itself, and transport() calls what it returns.
func (t Transports) constructor(broker bool) (func(config.NATS) (events.Transport, error), error) {
	if broker {
		if t.JetStream == nil {
			return nil, errors.New("app: nats.transport selects jetstream and Options.Transports.JetStream is nil; the application supplies the constructor (kit/events/providers/nats.Connect)")
		}
		return t.JetStream, nil
	}
	if t.Memory == nil {
		return nil, errors.New("app: nats.transport selects memory and Options.Transports.Memory is nil; the application supplies the constructor (kit/events/providers/memory.New)")
	}
	return func(config.NATS) (events.Transport, error) { return t.Memory(), nil }, nil
}

// Installation is the installation's own host: the address that serves the
// control plane. One field, because one fact is what the surface needs — which
// host answers /ops — and a value with more would be a field nothing reads.
type Installation struct {
	Host string
}

// App is a composed application that has not started yet.
type App struct {
	cfg  config.Config
	mods []module.Module
	opts Options
	log  *slog.Logger

	// declaredRoutes is the composition's route signature as the first of its four
	// registrations left it, and nil exactly when no registration of this composition
	// has run yet. registeredRoutes and registrationsAgree are its whole life: the
	// first registration writes it and each of the other three is judged against it.
	declaredRoutes  []string
	declaredScreens []byte
	screenInventory screen.Inventory

	// declared is every event these manifests promise, with the payload type each
	// one named, and catalog is this composition's own copy of that promise: the
	// value kit/events measures every publish it carries against, and the grip it
	// holds on those names in the process-wide record other publishes are measured
	// against (kit/events/catalog.go, kit/events/publisher.go). A composition's
	// tenants are therefore measured against that composition's own contract from the
	// moment the composition exists, and a boot can neither replace nor erase what
	// another app is answering under. Close gives back what this composition added —
	// every refusal after New, and the Close of the Runtime Start hands out — which is
	// what keeps a build this kernel refused from holding shapes no live composition
	// declares.
	declared        []events.Declared
	catalog         *events.Catalog
	declaredRelease func()

	// held is this composition's connection, held open-handed from New until Start
	// opens it. The API, its readiness probe, its anonymous write counter and the
	// record of its refusals are all built before the pool is dialled — see heldConn
	// — and each of them asks this for the connection on the request that needs it.
	held *heldConn

	// observed is the event transport this composition's live connections watch:
	// Start's, handed over the same way, and otherwise the one the live door dials
	// for a role that runs no worker half. It is one transport rather than one per
	// connection because the outbox relay publishes on one — see heldTransport.
	observed *heldTransport

	// measurement is this deployment's telemetry, planned here and installed by a
	// Start that got all the way through. Batched spans and the last metric interval
	// have to be pushed before the process is gone, because a collector nobody told
	// about does not come and fetch them — and a composition that was refused never
	// asked to be traced anywhere. Nothing but a whole lifecycle needs either half,
	// so they are held here rather than passed around — see flushTelemetry for who
	// calls the flush.
	measurement *telemetryPlan

	// reports are what /ready says about the parts of the process that are not
	// verdicts — today, the exporter's last success. Empty when measurement is off.
	reports []health.Report

	// telemetryOnce makes the flush one per process. Run and Runtime.Close both
	// end a lifecycle and Run does both of them, so without it a traced process
	// would shut its providers down twice and log the same failure twice.
	telemetryOnce sync.Once
}

func (a *App) serve(ctx context.Context, h http.Handler) error {
	srv := &http.Server{
		Addr:    a.cfg.Server.Addr,
		Handler: h,
		// Bound incomplete requests and idle clients. ReadTimeout is configurable
		// so deployments can accommodate their upload size and connection speeds.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       a.cfg.Server.ReadTimeout,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopped := make(chan error, 1)
	go func() { stopped <- srv.ListenAndServe() }()
	a.log.InfoContext(ctx, "app: listening", "addr", a.cfg.Server.Addr, "role", a.opts.Role)

	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("app: serve: %w", err)
	case <-ctx.Done():
		grace, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(grace); err != nil {
			return fmt.Errorf("app: shutdown: %w", err)
		}
		return nil
	}
}
