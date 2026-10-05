// roles.go is the list somebody wrote down: who a tenant of this application
// begins as, beside the two roles auth owns.
//
// It used to be derived. `compose` filled `personas` from `declaredRoles(mods)` —
// every role every composed manifest declares — because the task desk's
// coordinator is the task module's fact and this application only composes the
// module. That worked while compose ran before the sentence and could see the
// manifests it had just built. With the modules wiring themselves there is no
// such moment: at the time `Roles(…)` is named the manifests do not exist, and
// the one module that sees them is the one that runs after everything.
//
// So the list is written here, and the derivation moved into a case:
// TestEveryRoleEveryComposedModuleDeclaresIsOneThisApplicationSeeds reads the
// roles off the resolved composition and names the one this list omits. Same
// assertion, one direction round, and now over what the application actually
// resolved rather than over a slice a composition function happened to fill.
//
// The admin role (everything but the operator's) and the member role (nothing
// until somebody grants it) are auth's own and are not repeated here. What each
// persona may do, and what each is refused, is persona_test.go's table; which task
// a coordinator may resolve is policy/task.rego's, not a grant's.
package main

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

var personas = []authcontracts.Role{
	{Name: "coordinator", Grants: authcontracts.Permissions{
		taskcontracts.PermissionTaskRead, taskcontracts.PermissionTaskUpdate,
	}},
	{Name: "observer", Grants: authcontracts.Permissions{taskcontracts.PermissionTaskRead}},
}

// seedRoles provisions auth's defaults and this application's personas in the
// tenant's creation transaction. Operator grants are named by the application that
// composes their owners, which is why this is a hook the product contributes and
// not something the tenant module could know.
func seedRoles(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	return auth.SeedRoles(ctx, tx, t.Tenancy(), []string{
		tenantcontracts.PermissionTenantManage,
		billingcontracts.PermissionBillingCatalog,
	}, personas)
}
