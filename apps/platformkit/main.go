// Command platformkit is the reference application: one binary, one image, and
// a --role flag that says which half of the work this process does.
//
// It is short on purpose. Everything it does is read a configuration, choose
// the three implementations the kernel cannot choose for itself, hand kit/app
// the list of modules, and run until something stops it. There is no framework
// between this file and the modules it composes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

func main() {
	path := flag.String("config", "config.yaml", "Path to the configuration file")
	role := flag.String("role", string(app.All), "web, worker, or all")
	flag.Parse()

	if err := run(*path, app.Role(*role)); err != nil {
		// The error goes to stderr rather than through the logger, because the
		// failures this returns include the ones that happen before there is a
		// configured logger to write to.
		fmt.Fprintln(os.Stderr, "platformkit:", err)
		os.Exit(1)
	}
}

// run is main with an error return, so every failure has one exit and the
// deferred work still happens.
func run(path string, role app.Role) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)

	// SIGINT and SIGTERM cancel the context every part of the application is
	// running under, which is how a rolling deploy drains: kit/app stops
	// listening, finishes what is in flight, and returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The development tenancy and identity, until E3. See dev.go.
	if !cfg.Dev.Enabled {
		return fmt.Errorf("dev.enabled is false, and the tenant and auth modules that would replace it arrive in stage E3; see config.example.yaml")
	}
	d, err := newDev(cfg)
	if err != nil {
		return err
	}

	a, err := app.New(ctx, cfg, modules(d), app.Options{
		Tenants:      d,
		Authorize:    d,
		Authenticate: d.Authenticate,
		Role:         role,
	})
	if err != nil {
		return err
	}
	return a.Run(ctx)
}

// logger sets the process's default logger: JSON on stderr, at the configured
// level. It is the default and not a value passed anywhere — kit/app builds the
// same logger from the same key — because the kernel's packages that take no
// logger log through slog.Default, and a process whose two loggers disagreed
// about the level would be a log with a hole in it.
func logger(level string) {
	var l slog.Level
	// config.Load has already refused anything outside the four, so the error
	// here cannot happen; ignoring it would leave the default, which is info.
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}
