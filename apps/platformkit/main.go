// Command platformkit is the reference application: one binary, one image, and
// two subcommands — `run`, which serves, and `bootstrap`, which creates the
// first tenant of an empty installation.
//
// It is short on purpose. Everything it does is read a configuration, compose
// the modules, choose the three implementations the kernel cannot choose for
// itself, and run until something stops it. There is no framework between this
// file and the modules it composes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

func main() {
	// `platformkit` alone is `platformkit run`, because running is what the
	// image does and an entrypoint should not need an argument. A first
	// argument that is not a flag is the subcommand.
	command, args := "run", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	var err error
	switch command {
	case "run":
		err = run(args)
	case "bootstrap":
		err = bootstrap(args)
	case "start":
		err = startApp(args)
	default:
		err = fmt.Errorf("%q is not a command; there are three: run, bootstrap and start", command)
	}
	if err != nil {
		// The error goes to stderr rather than through the logger, because the
		// failures this returns include the ones that happen before there is a
		// configured logger to write to.
		fmt.Fprintln(os.Stderr, "platformkit:", err)
		os.Exit(1)
	}
}

// run serves. It is main with an error return, so every failure has one exit
// and the deferred work still happens.
func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	role := fs.String("role", string(app.All), "web, worker, or all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)

	// SIGINT and SIGTERM cancel the context every part of the application is
	// running under, which is how a rolling deploy drains: kit/app stops
	// listening, finishes what is in flight, and returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := compose(cfg)
	if !cfg.Mail.Enabled() {
		// Said out loud, because the failure it warns about is silent: every
		// notification is still written and still visible in the application,
		// and the ones asking for mail are logged instead of sent.
		slog.WarnContext(ctx, "app: mail is not configured, so notifications marked for email are recorded and not sent; set mail.host")
	}
	a, err := app.New(ctx, cfg, c.modules, app.Options{
		Tenants:      c.tenants,
		Authorize:    c.auth,
		Authenticate: c.auth.Authenticate,
		Role:         app.Role(*role),
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
