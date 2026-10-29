package billing_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/billing"
)

// declaredCrossTenantAccess is the cross-tenant need this module's manifest
// states, handed to the raw httpx.API the harness below builds itself. In an
// application kit/app carries that sentence from the manifest to the kernel, and
// a test that mounts the module's Routes on an API of its own has to state the
// same fact: a token taken without a declaration is refused at composition.
func declaredCrossTenantAccess() map[string]string {
	return map[string]string{"billing": "read every tenant's subscription before a plan is removed"}
}

// TestAPlanDeleteDeclaresTheWorkItCrossesTenantsFor pins the sentence above to the
// manifest it copies: the reason is the audit record of every cross-tenant
// transaction the module opens, and a harness handing the kernel a different one
// would be testing a composition nobody ships.
func TestAPlanDeleteDeclaresTheWorkItCrossesTenantsFor(t *testing.T) {
	_, m := billing.New(billing.Deps{Payments: billing.Manual()})
	if got, want := m.SystemAccess.Reason, declaredCrossTenantAccess()["billing"]; got != want {
		t.Fatalf("the manifest declares cross-tenant work as %q; the mounted routes take a token minted for %q", got, want)
	}
}
