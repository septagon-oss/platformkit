package pkit_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestRunRefusesCanonicalHostCollisionBeforeMigration(t *testing.T) {
	cfg := onOneDatabase(t)
	server := pkit.NewServer().Config(cfg).Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk),
			pkit.Tenant("Acme", "ACME.Test:8080"),
			pkit.Tenant("Globex", "acme.test."))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := server.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "Acme") ||
		!strings.Contains(err.Error(), "Globex") ||
		!strings.Contains(err.Error(), "acme.test") {
		t.Errorf("Run did not refuse the shared host with both tenant names: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Run migrated %d tables before refusing the host collision", got)
	}
}
