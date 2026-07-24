// Package main is the domain-neutral PlatformKit OSS front door. It runs the
// stable nine-module starter exactly as published by pk-apps. Product-specific
// modules belong in downstream applications and compose through supported
// starterapp extension points.
//
// The front door ships no config.yaml; starterapp.DefaultConfig returns a
// complete, bootable configuration so `go run .` works out of the box.
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

func run(ctx context.Context, lookup func(string) string, start starterRunner) error {
	cfg := starterapp.DefaultConfig()
	starterapp.ApplyAddressOverrides(cfg, lookup)
	return start(ctx, cfg)
}
