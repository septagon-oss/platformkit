package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/septagon-oss/platformkit/kit/appname"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/module"
)

// level is config's log.level as a slog.Level. config.Load has already refused
// anything outside the four names, so the fallback is unreachable and is the
// least surprising of the four.
func level(name string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(name)); err != nil {
		return slog.LevelInfo
	}
	return l
}

// New checks the composition and returns the application it describes. Every
// error it can return is a wiring mistake, so they all surface before anything
// is opened, listened on or migrated.
func New(ctx context.Context, cfg config.Config, mods []module.Module, opts Options) (*App, error) {
	if err := defaultOptions(&opts); err != nil {
		return nil, err
	}
	if err := reconcileName(&cfg, &opts); err != nil {
		return nil, err
	}
	if err := validateDeployment(cfg, opts); err != nil {
		return nil, err
	}
	// Expand subscriptions after adding every emitter, including the kernel.
	mods = append(mods, kernelModule(opts.App))
	mods = module.Expand(mods)
	if err := module.Validate(mods); err != nil {
		return nil, err
	}
	// Refuse conflicting payload shapes before claiming process state or opening resources.
	if err := events.CheckAppDeclared(opts.App, allDeclaredEvents(mods)); err != nil {
		return nil, beforeEffects(err)
	}
	declared := declaredEvents(mods)
	log := compositionLog(ctx, cfg, mods, opts)
	// Plan telemetry now; Start installs it only after every fallible boot step.
	measurement, report, err := planTelemetry(ctx, cfg.Telemetry, log)
	if err != nil {
		return nil, err
	}
	var reports []health.Report
	if report != nil {
		reports = append(reports, report)
	}
	// Claim shapes last, under the catalog lock, so a refused constructor keeps no
	// claim. The claim is a value rather than a release closure because the
	// composition is the only thing that knows which of its own publishes belongs to
	// which contract: it hands the value to the doors it owns (start.go, workers.go)
	// and the process-wide record keeps answering for every door it does not.
	catalog, err := events.NewCatalog(opts.App, declared)
	if err != nil {
		return nil, beforeEffects(err)
	}
	return &App{cfg: cfg, mods: mods, opts: opts, log: log, declared: declared,
		catalog: catalog, declaredRelease: catalog.Close,
		held: &heldConn{}, observed: &heldTransport{}, measurement: measurement, reports: reports}, nil
}

func defaultOptions(opts *Options) error {
	if opts.Clock == nil {
		opts.Clock = db.Now
	}
	switch {
	case opts.Tenants == nil:
		return errors.New("app: Options.Tenants is required")
	case opts.Authorize == nil:
		return errors.New("app: Options.Authorize is required")
	case opts.Authenticate == nil:
		return errors.New("app: Options.Authenticate is required")
	}
	if opts.Role == "" {
		opts.Role = All
	}
	return nil
}

func reconcileName(cfg *config.Config, opts *Options) error {
	// Configuration and code must name the same application for every consumer.
	configured, err := cfg.NATS.AppName()
	if err != nil {
		// Malformed names must fail even when the selected transport never reads NATS settings.
		return fmt.Errorf("app: %w", err)
	}
	if opts.App.Named() {
		// A direct Name conversion can bypass Parse. Validate here so tenants,
		// transport names and payload declarations all use a valid app identity.
		if _, err := appname.Parse(string(opts.App)); err != nil {
			return fmt.Errorf("app: Options.App %q: %w", string(opts.App), err)
		}
		if configured.Named() && configured != opts.App {
			// Conflicting names would scope tenants and events to different apps;
			// the deployment must choose one identity.
			return fmt.Errorf("app: Options.App %q and nats.app %q are two answers to which app this composition is", string(opts.App), cfg.NATS.App)
		}
		if !configured.Named() {
			// Copy the code-supplied name into configuration so transport and
			// migration consumers use it too. The caller retains its original Config.
			cfg.NATS.App = string(opts.App)
		}
	} else {
		// With no code-supplied name, use the deployment identity.
		opts.App = configured
	}
	return nil
}

func validateDeployment(cfg config.Config, opts Options) error {
	switch opts.Role {
	case Web, Worker, All:
	default:
		return fmt.Errorf("app: role %q is not one of %q, %q or %q", opts.Role, Web, Worker, All)
	}
	pool := databasePool(cfg.Database)
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	if pool.MaxOpenConns < 2 {
		return errors.New("app: database.max_open_conns must be at least two: authenticated HTTP handlers open detached transactions, and scheduled jobs hold an advisory-lock connection")
	}
	if opts.Transport == nil {
		broker, err := useJetStream(cfg.NATS.Transport, opts.Role)
		if err != nil {
			return err
		}
		if _, err := opts.Transports.constructor(broker); err != nil {
			return err
		}
		if broker {
			if err := cfg.NATS.Validate(); err != nil {
				return fmt.Errorf("app: %w", err)
			}
		}
	}
	// Check the cache constructor in every role so web and worker agree
	// on whether this deployment can start.
	if _, err := opts.Caches.constructor(cfg.Cache.Adapter); err != nil {
		return err
	}
	return nil
}

func compositionLog(ctx context.Context, cfg config.Config, mods []module.Module, opts Options) *slog.Logger {
	log := opts.Log
	if log == nil {
		// Use the configured level unless the caller supplied its own logger.
		log = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level(cfg.Log.Level)}))
	}
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name)
	}
	log.InfoContext(ctx, "app: composed", "modules", names, "role", opts.Role)
	if opts.Role == All && opts.Transport == nil && cfg.NATS.Transport != "jetstream" {
		log.WarnContext(ctx, "app: in-process events reach only this replica; set nats.transport to jetstream to share events between replicas")
	}
	return log
}
