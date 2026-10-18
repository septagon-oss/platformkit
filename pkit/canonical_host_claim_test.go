package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestServerRefusesTenantsClaimingTheSameCanonicalHost(t *testing.T) {
	cfg := onOneDatabase(t)
	server := pkit.NewServer().Config(cfg).
		Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk),
			pkit.Tenant("Acme", "ACME.Test"),
			pkit.Tenant("Globex", "acme.test."))

	served, err := server.Build(t.Context())
	if served != nil {
		defer served.Close()
		t.Error("Build returned a server for two tenants claiming the same request host")
	}
	if err == nil {
		t.Error("Build accepted two tenants claiming the same request host")
	} else if !strings.Contains(err.Error(), "Acme") ||
		!strings.Contains(err.Error(), "Globex") ||
		!strings.Contains(strings.ToLower(err.Error()), "acme.test") {
		t.Errorf("the host collision refusal did not name both tenants and their host: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Build migrated %d tables before refusing the host collision", got)
	}
}
