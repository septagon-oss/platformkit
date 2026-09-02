// Package app assembles a running application from a configuration and a list
// of modules.
//
// New checks the composition; Run applies the migrations, opens the connection,
// builds the API, lets each module register its routes, proves that every
// registered operation declared its authorization, and then serves until the
// context is done. The order is written here, once, in the order it happens —
// which is the whole argument of docs/adr/0002: a startup sequence that is read
// rather than derived.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Options are the cross-cutting implementations main chooses. They are the
// three questions the kernel cannot answer for itself — which host is which
// tenant, who is calling, and what they may do — plus where to log.
type Options struct {
	Tenants      tenancy.Resolver
	Authorize    httpx.Authorizer
	Authenticate func(r *http.Request) (httpx.Principal, bool)
	Log          *slog.Logger
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
	log.InfoContext(ctx, "app: composed", "modules", names)
	return &App{cfg: cfg, mods: mods, opts: opts, log: log}, nil
}

// Run migrates, serves, and returns when ctx is done or the server stops.
//
// It is single-role: web. --role web|worker arrives with kit/jobs in E1.3.
func (a *App) Run(ctx context.Context) error {
	// 1. The ledger, as the owner role. Every module's SQL is applied in list
	//    order through the one ledger; see sources.
	src, err := sources(a.mods)
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, a.cfg.Database.MigrateURL, src); err != nil {
		return err
	}

	// 2. The application connection, as the role row-level security binds.
	conn, err := db.Open(ctx, a.cfg.Database.URL)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 3. The API and its middleware chain.
	api, router := httpx.New(httpx.Options{
		PublicHost:   a.cfg.Server.PublicHost,
		Tenants:      a.opts.Tenants,
		Conn:         conn,
		Authorize:    a.opts.Authorize,
		Authenticate: a.opts.Authenticate,
		Log:          a.log,
	})

	// 4. The routes and the checks, module by module, in list order.
	checks := []health.Check{health.DatabaseCheck(conn)}
	for _, m := range a.mods {
		if m.Routes != nil {
			m.Routes(api)
		}
		checks = append(checks, m.Health...)
	}
	health.Register(api, checks...)

	// 5. The gate. An operation that declares no authorization fails startup,
	//    with no warn mode: this repository has no deployment to migrate and no
	//    reason to run a build it knows is unprotected.
	if err := api.ValidateDeclarations(); err != nil {
		return err
	}
	a.log.InfoContext(ctx, "app: operations declared", "count", len(api.Recorded()),
		"public_mutations", api.PublicMutations())

	// 6. Serve.
	return a.serve(ctx, router)
}

func (a *App) serve(ctx context.Context, h http.Handler) error {
	srv := &http.Server{
		Addr:              a.cfg.Server.Addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	stopped := make(chan error, 1)
	go func() { stopped <- srv.ListenAndServe() }()
	a.log.InfoContext(ctx, "app: listening", "addr", a.cfg.Server.Addr)

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
