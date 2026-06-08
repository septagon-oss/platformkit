// Package main is the PlatformKit OSS front door: a thin wrapper over the
// importable starterapp package. It carries no application logic of its own —
// the entire module composition graph, HTTP surface, first-boot seed, and serve
// loop live in github.com/septagon-oss/pk-apps/pkg/starterapp, the same package
// that pk-apps's own apps/starter-saas/main.go wraps. Cloning this repo and
// running `go run .` boots the identical OSS monolith on :8080.
//
// The front door ships no config.yaml; starterapp.DefaultConfig returns a
// complete, bootable configuration so `go run .` works out of the box.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition
// through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	// Register the modernc.org/sqlite driver as "sqlite" so each module's
	// store can sql.Open against the same default driver.
	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := starterapp.DefaultConfig()

	if err := starterapp.Run(ctx, cfg); err != nil {
		log.Fatalf("platformkit: %v", err)
	}
}
