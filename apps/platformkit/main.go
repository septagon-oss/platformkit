// Command platformkit is the reference application: one binary, one image, and
// four subcommands — `run`, which serves, `bootstrap`, which creates the first
// tenant of an empty installation, `start`, which runs the whole thing from
// nothing on a laptop, and `migrate`, which applies the pending schema and exits.
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
	"github.com/septagon-oss/platformkit/pkit"
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
	case "migrate":
		err = migrate(args)
	default:
		err = fmt.Errorf("%q is not a command; there are four: run, bootstrap, start and migrate", command)
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
	env := fs.String("environment", string(pkit.Production), "development, staging, or production: which deployment this is, which decides which implementation an app that names one runs")
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

	deployment := environment(*env)
	c := composeReference(cfg, deployment)
	switch {
	case cfg.Mail.Enabled():
		// A server was named, which is the whole answer: whatever this application
		// is asked to send, something outside this process sends it.
	case deployment == pkit.Development:
		// The other mail-less state, and it is worth one line because the two are
		// otherwise indistinguishable from the outside: a development deployment
		// that names no relay is answered by the notification module's simulated
		// mailbox, so a sign-up is accepted and its link waits in this process for
		// whoever is running it to read it.
		slog.WarnContext(ctx, "app: mail.host is empty and this is a development deployment, so every message this application is asked to send is kept in its own process and none leaves it; set mail.host to send it")
	default:
		// Said out loud, because the two failures it warns about are otherwise
		// quiet: a stranger who asks to sign up is refused with a reasoned 503 and
		// no row is written, and a notice that asks for mail is recorded as
		// suppressed in the delivery ledger.
		slog.WarnContext(ctx, "app: mail is not configured, so an emailed verification link cannot be sent: sign-up by email is refused and notifications marked for email are recorded as suppressed; set mail.host to send it")
	}
	return c.app.Run(ctx, c.once, app.Role(*role))
}

// environment reads the flag into pkit's name for it. An environment nobody named
// is refused by pkit rather than defaulted: the same composition runs a
// different implementation in development than in production, and a process that
// guessed which it was would be guessing about money.
func environment(name string) pkit.Environment {
	switch e := pkit.Environment(name); e {
	case pkit.Development, pkit.Staging, pkit.Production:
		return e
	default:
		return ""
	}
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
