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
	"strings"
	"syscall"

	// Register the modernc.org/sqlite driver as "sqlite" so the starter and
	// contributed modules can use the shared database handle.
	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := starterapp.DefaultConfig()

	applyAddressOverrides(cfg, os.Getenv)

	if err := starterapp.Run(ctx, cfg); err != nil {
		log.Fatalf("platformkit: %v", err)
	}
}

func applyAddressOverrides(cfg *starterapp.Config, lookup func(string) string) {
	if addr := strings.TrimSpace(lookup("PK_HTTP_ADDR")); addr != "" {
		cfg.HTTP.Addr = addr
		return
	}
	if port := strings.TrimSpace(lookup("PORT")); port != "" {
		// PORT changes the port, not the security posture. Binding all
		// interfaces requires an explicit PK_HTTP_ADDR.
		cfg.HTTP.Addr = "127.0.0.1:" + port
	}
}
