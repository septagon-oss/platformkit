package tenant_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/tenant"
)

// The directory with no hooks contributed: a tenant created here carries only
// its own row, which is a decision rather than a missing port.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := tenant.NewChecked(tenant.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the directory service is nil")
	}
	if m.Name != "tenant" {
		t.Errorf("manifest name = %q, want tenant", m.Name)
	}
}
