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

	// Register the modernc.org/sqlite driver as "sqlite" so the starter and
	// contributed modules can use the shared database handle.
	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, os.Getenv, starterapp.Run); err != nil {
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

func run(ctx context.Context, lookup func(string) string, start starterRunner) error {
	cfg := starterapp.DefaultConfig()
	if path := configPath(lookup); path != "" {
		loaded, err := starterapp.LoadConfig(path)
		if err != nil {
			return err
		}
		cfg = loaded
	}
	starterapp.ApplyAddressOverrides(cfg, lookup)
	return start(ctx, cfg)
}
