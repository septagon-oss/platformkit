// Package main is the domain-neutral PlatformKit OSS front door. It runs the
// stable nine-module starter exactly as published by pk-apps. Product-specific
// modules belong in downstream applications and compose through supported
// starterapp extension points.
//
// The front door ships no config.yaml; starterapp.DefaultConfig returns a
// complete, bootable configuration so `go run .` works out of the box. When a
// config.yaml sits next to the process (or PK_CONFIG names one), it is loaded
// through starterapp.LoadConfig, which fails closed to production — that is
// how a released binary reaches environment=production and seed.admin_password
// without a downstream Go wrapper.
//
// The binary is a small cobra CLI (see cli.go): running with no subcommand
// serves, exactly as every release before it did.
//
// Implements: REQ-016.
// Per: ADR-0017, ADR-0029.
// Discipline: C-14.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	// Register both supported drivers so `database.driver` selects an engine
	// with no code change: "sqlite" (embedded, the zero-setup default) and
	// "postgres"/"pgx" (the production profile). Registering a driver only adds
	// it to database/sql's table — the unused one costs a few hundred KB of
	// binary and nothing at runtime.
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		log.Fatalf("platformkit: %v", err)
	}
}

type starterRunner func(context.Context, *starterapp.Config, ...starterapp.Option) error

// configPath resolves the optional configuration file: PK_CONFIG wins, then a
// config.yaml in the working directory. Empty means zero-config development
// mode (starterapp.DefaultConfig), which stays loopback-only and seeded.
func configPath(lookup func(string) string) string {
	if p := lookup("PK_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	return ""
}

// run is the historical zero-flag boot path: resolve the optional config
// file, apply environment overrides, and start. Kept as the seam the boot
// tests exercise; the CLI reaches the same code through runWith.
func run(ctx context.Context, lookup func(string) string, start starterRunner) error {
	return runWith(ctx, lookup, start, serveFlags{})
}
