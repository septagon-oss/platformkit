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

	// Register the modernc.org/sqlite driver as "sqlite" so each module's
	// store can sql.Open against the same default driver.
	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := starterapp.DefaultConfig()

	// Let the listen address be overridden without editing code:
	//   PORT=8090 go run .            (or)   PK_HTTP_ADDR=127.0.0.1:8090 go run .
	// PORT is the conventional PaaS/container knob; PK_HTTP_ADDR takes a full
	// host:port when you need to bind a specific interface.
	if addr := strings.TrimSpace(os.Getenv("PK_HTTP_ADDR")); addr != "" {
		cfg.HTTP.Addr = addr
	} else if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		cfg.HTTP.Addr = ":" + port
	}

	if err := starterapp.Run(ctx, cfg); err != nil {
		log.Fatalf("platformkit: %v", err)
	}
}
