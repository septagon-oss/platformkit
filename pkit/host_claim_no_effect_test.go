package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

// A conflicting host claim must be answered before the engine migrates the
// database. The host is only a claim; no tenant row can resolve the conflict.
func TestTwoTenantClaimsOnOneHostLeaveTheDatabaseUntouched(t *testing.T) {
	cfg := onOneDatabase(t)
	s := pkit.NewServer().Config(cfg).Deploy(pkit.Deployment{Environment: pkit.Development}).
		Host(pkit.NewApp("collect").Use(doors, desk),
			pkit.Tenant("Acme", "shared.test"), pkit.Tenant("Globex", "shared.test"))

	served, err := s.Build(t.Context())
	if served != nil || err == nil || !strings.Contains(err.Error(), "shared.test") ||
		!strings.Contains(err.Error(), "Acme") || !strings.Contains(err.Error(), "Globex") {
		t.Fatalf("conflicting claims should refuse both tenants and return no runtime: runtime=%v, err=%v", served, err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("a refused host claim left %d tables behind", got)
	}
}
