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
// that is read rather than derived.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
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
// who is calling, and what they may do — plus the role, the event transport,
// the tenant list the periodic jobs walk, and where to log.
type Options struct {
	Tenants      httpx.TenantLoader
	Authorize    httpx.Authorizer
	Authenticate func(r *http.Request) (httpx.Principal, bool)
	Log          *slog.Logger

	// Role defaults to All.
	Role Role

	// Transport carries events between the relay and the handlers. It defaults
	// to events.Memory() for All, which needs no broker because there is no
	// second process, and to JetStream on config's nats.url otherwise.
	Transport events.Transport

	// AllTenants lists every tenant, for jobs that walk them. The tenant module
	// implements it; a job that needs it and does not have it fails loudly
	// rather than quietly doing nothing.
	AllTenants jobs.TenantLister
}

// App is a composed application that has not started yet.
type App struct {
	cfg  config.Config
	mods []module.Module
	opts Options
	log  *slog.Logger
}

// shutdownGrace bounds the wait for in-flight requests once the context is done.
const shutdownGrace = 10 * time.Second

// The built-in jobs, in every worker. The relay is a second because an event is
// asynchronous, not slow; the purge is hourly because a week of history does
// not need attention more often than that.
const (
	relayEvery = time.Second
	purgeCron  = "0 * * * *"
)

// New checks the composition and returns the application it describes. Every
// error it can return is a wiring mistake, so they all surface before anything
// is opened, listened on or migrated.
func New(ctx context.Context, cfg config.Config, mods []module.Module, opts Options) (*App, error) {
	switch {
	case opts.Tenants == nil:
		return nil, errors.New("app: Options.Tenants is required")
	case opts.Authorize == nil:
		return nil, errors.New("app: Options.Authorize is required")
	case opts.Authenticate == nil:
		return nil, errors.New("app: Options.Authenticate is required")
	}
	if opts.Role == "" {
		opts.Role = All
	}
	switch opts.Role {
	case Web, Worker, All:
	default:
		return nil, fmt.Errorf("app: role %q is not one of %q, %q or %q", opts.Role, Web, Worker, All)
	}
	if err := module.Validate(mods); err != nil {
		return nil, err
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name)
	}
	log.InfoContext(ctx, "app: composed", "modules", names, "role", opts.Role)
	return &App{cfg: cfg, mods: mods, opts: opts, log: log}, nil
}

// Run migrates, then serves or works or both, and returns when ctx is done.
func (a *App) Run(ctx context.Context) error {
	if err := a.migrate(ctx); err != nil {
		return err
	}
	conn, err := a.openConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if a.opts.Role == Web {
		_, router, err := a.buildAPI(ctx, conn)
		if err != nil {
			return err
		}
		return a.serve(ctx, router)
	}

	transport, err := a.transport()
	if err != nil {
		return err
	}
	if closer, ok := transport.(io.Closer); ok {
		defer closer.Close()
	}
	if a.opts.Role == Worker {
		return a.work(ctx, conn, transport, a.probes(conn))
	}

	_, router, err := a.buildAPI(ctx, conn)
	if err != nil {
		return err
	}
	// One process, both halves. The web half owns the listener, so the worker
	// half is given no handler of its own.
	return a.both(ctx, conn, transport, router)
}

// migrate applies the ledger as the owner role. Every role does, worker
// included: the advisory lock makes the race safe, which removes the ordering
// problem instead of sequencing it. See docs/adr/0005.
func (a *App) migrate(ctx context.Context) error {
	src, err := sources(a.mods)
	if err != nil {
		return err
	}
	return db.Migrate(ctx, a.cfg.Database.MigrateURL, src)
}

// openConn opens the application connection, as the role row-level security
// binds.
func (a *App) openConn(ctx context.Context) (*db.Conn, error) {
	return db.Open(ctx, a.cfg.Database.URL)
}

// transport is the event transport this role uses.
func (a *App) transport() (events.Transport, error) {
	if a.opts.Transport != nil {
		return a.opts.Transport, nil
	}
	if a.opts.Role == All {
		return events.Memory(), nil
	}
	return events.JetStream(a.cfg.NATS.URL)
}

// buildAPI builds the API, lets every module register its routes and checks,
// and runs the boot gates. It returns before anything listens, so a composition
// that fails a gate never takes the port.
func (a *App) buildAPI(ctx context.Context, conn *db.Conn) (*httpx.API, http.Handler, error) {
	api, router := httpx.New(httpx.Options{
		PublicHost:   a.cfg.Server.PublicHost,
		Docs:         a.cfg.Server.Docs,
		Tenants:      a.opts.Tenants,
		Conn:         conn,
		Authorize:    a.opts.Authorize,
		Authenticate: a.opts.Authenticate,
		Log:          a.log,
	})

	checks := []health.Check{health.DatabaseCheck(conn)}
	for _, m := range a.mods {
		if m.Routes != nil {
			m.Routes(api)
		}
		checks = append(checks, m.Health...)
	}
	health.Register(api, checks...)

	// The gates. An operation that declares no authorization, one guarded by a
	// permission no module defines, or one that would publish an event no
	// module promised, fails startup — with no warn mode: this repository has
	// no deployment to migrate and no reason to run a build it knows is
	// unprotected, unreachable or unannounced.
	if err := api.ValidateDeclarations(); err != nil {
		return nil, nil, err
	}
	if err := validatePermissions(api, a.mods); err != nil {
		return nil, nil, err
	}
	if err := validateEvents(api, a.mods); err != nil {
		return nil, nil, err
	}
	a.log.InfoContext(ctx, "app: operations declared", "count", len(api.Recorded()), "events", len(api.Events()))
	return api, router, nil
}

// work is the worker role: the outbox relay, the outbox purge, every module's
// jobs, and every module's subscriptions. probes is the handler it serves, or
// nil when the web half of the same process is already serving them.
func (a *App) work(ctx context.Context, conn *db.Conn, transport events.Transport, probes http.Handler) error {
	scheduled := []jobs.Job{
		{Name: "outbox-relay", Every: relayEvery, Run: func(ctx context.Context) error {
			return events.Relay(ctx, conn, transport)
		}},
		{Name: "outbox-purge", Cron: purgeCron, Run: func(ctx context.Context) error {
			return events.Purge(ctx, conn)
		}},
	}
	var subs []events.Subscription
	for _, m := range a.mods {
		scheduled = append(scheduled, m.Jobs...)
		subs = append(subs, m.Subscriptions...)
	}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		return err
	}
	a.log.InfoContext(ctx, "app: working", "jobs", len(scheduled), "subscriptions", len(subs))

	scheduler := jobs.NewScheduler(conn, a.log, scheduled...)
	if probes == nil {
		return scheduler.Run(ctx)
	}
	return a.race(ctx,
		func(ctx context.Context) error { return scheduler.Run(ctx) },
		func(ctx context.Context) error { return a.serve(ctx, probes) })
}

// both runs the web half and the worker half in one process.
func (a *App) both(ctx context.Context, conn *db.Conn, transport events.Transport, router http.Handler) error {
	return a.race(ctx,
		func(ctx context.Context) error { return a.work(ctx, conn, transport, nil) },
		func(ctx context.Context) error { return a.serve(ctx, router) })
}

// race runs two halves and returns the first failure, having stopped the other.
// Either half stopping means this process is stopping: there is no half of an
// application worth keeping alive on its own.
func (a *App) race(ctx context.Context, halves ...func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, len(halves))
	for _, half := range halves {
		go func() { done <- half(ctx) }()
	}
	first := <-done
	stop()
	for range halves[1:] {
		<-done
	}
	return first
}

// probes is the worker's whole HTTP surface: liveness and readiness, on the
// same address the web role listens on, so one orchestrator manifest describes
// both roles. It is a plain mux rather than an API because a worker has no
// tenant, no session and no operation to declare.
func (a *App) probes(conn *db.Conn) http.Handler {
	checks := []health.Check{health.DatabaseCheck(conn)}
	for _, m := range a.mods {
		checks = append(checks, m.Health...)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		var failed []string
		for _, c := range checks {
			if err := c.Check(r.Context()); err != nil {
				a.log.ErrorContext(r.Context(), "app: check failed", "check", c.Name(), "error", err)
				failed = append(failed, c.Name())
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if len(failed) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"status":"not ready: %s"}`, strings.Join(failed, ", "))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// validatePermissions is the declaration gate's mirror: every permission a
// route requires has to be one some module defines, or the route is guarded by
// a token no role can ever be granted and everybody is denied for good. Every
// missing one is reported at once, because a composition is fixed once.
func validatePermissions(api *httpx.API, mods []module.Module) error {
	defined := map[string]bool{}
	for _, m := range mods {
		for _, p := range m.Permissions {
			defined[p.Key] = true
		}
	}
	var missing []string
	for _, p := range api.Permissions() {
		if !defined[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("app: %d permission(s) guard a route and are defined by no module: %s",
		len(missing), strings.Join(missing, ", "))
}

// validateEvents is the same gate for the other direction: an operation that
// will publish an event no module declared is an event no subscriber can be
// written against, because the manifest is where a subscriber looks.
func validateEvents(api *httpx.API, mods []module.Module) error {
	declared := map[string]bool{}
	for _, m := range mods {
		for _, e := range m.Events {
			declared[e] = true
		}
	}
	var missing []string
	for _, e := range api.Events() {
		if !declared[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("app: %d event(s) would be published by a route and are declared by no module: %s",
		len(missing), strings.Join(missing, ", "))
}

func (a *App) serve(ctx context.Context, h http.Handler) error {
	srv := &http.Server{
		Addr:    a.cfg.Server.Addr,
		Handler: h,
		// A request holds a database transaction, so a slow client holds one
		// too: these bound how long one can. The read header timeout stops a
		// connection that never finishes its request line; the write timeout
		// stops a client that never reads its response; the idle timeout
		// reclaims keep-alive connections. They are constants because no
		// deployment has had a reason to differ.
		ReadHeaderTimeout: 10 * time.Second,
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
