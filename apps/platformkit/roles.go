// roles.go is the list somebody wrote down: who a tenant of this application
// begins as, beside the two roles auth owns.
//
// It used to be derived. `compose` filled `initialRoles` from `declaredRoles(mods)`
// — every role every composed manifest declares — because the task desk's
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
// until somebody grants it) are auth's own and are not repeated here. A module
// that adds two doors of its own — webhook:read and webhook:update are the newest —
// is answered by that pair and not by a line here: admin holds every tenant-scoped
// permission, which is how a tenant administrator registers an endpoint on day one,
// and these two personas are the task module's, declared and granted to match what
// that module declares. Adding webhook words to them would be a grant nobody asked
// for and a persona test that can no longer refuse anything. What each
// persona may do, and what each is refused, is the persona table's; which task
// a coordinator may resolve is policy/task.rego's, not a grant's.
//
// What is *not* in this file is the operator half of a new administrator's grants.
// That used to be a two-element literal here, naming two permissions this
// application happened to compose the owners of, which is the defect: a product
// built from this sentence without the module owning one seeded the grant and
// could not exercise it, and only the hourly sweep said so, an hour late, forever.
// seedRoles reads those grants off the manifests of the modules this application
// actually composes, and an initial role naming a permission no composed module
// defines is refused where it used to be written.
package main

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// initialRoles is what this application seeds beyond the two built-in ones, and
// the one list two readers ask for: the hook below, which decides what a new
// tenant's roles hold, and repair-roles, which may take away only what that
// seeder could have written. One list rather than two that happen to agree,
// because the repair's whole permission to touch a role comes from being the
// seeder's.
var initialRoles = []authcontracts.Role{
	{Name: "coordinator", Grants: authcontracts.Permissions{
		taskcontracts.PermissionTaskRead, taskcontracts.PermissionTaskUpdate,
		// Nothing here names a person's kept copies. The pair is the person's own door,
		// which the module answers only inside that person's own boundary, so this role
		// is refused it on purpose and an acceptance case measures the refusal.
	}},
	{Name: "observer", Grants: authcontracts.Permissions{taskcontracts.PermissionTaskRead}},
	// The officer who answers a person's request on the tenant's behalf. The two grants
	// are the module's own declaration; which of this application's roles may erase is
	// this application's decision (ADR 0020, Limits), and the person's own right to ask
	// for either is the reason neither of them is how a person gets their own copy.
	{Name: "subject_officer", Grants: authcontracts.Permissions{
		subjectcontracts.PermissionExport, subjectcontracts.PermissionErase,
	}},
}

// seedRoles is this application's tenant-creation hook: auth's defaults plus the
// initial roles above, provisioned in the tenant's creation transaction. It is a
// hook the product contributes and not something the tenant module could know —
// but the operator permissions it seeds are no more this file's list than the
// personas are, so the hook is built from the composition's own catalogue.
//
// That catalogue arrives as a function rather than a list because the hook runs
// when a tenant is created, long after the sentence was written, and asks about
// modules the sentence has not resolved yet at the moment this is contributed:
// see app.go, which fills the holder this closes over once the plan is answered.
func seedRoles(catalogue func() []tenancy.Grant) tenantcontracts.Hook {
	return func(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
		return auth.SeedRoles(ctx, tx, t.Tenancy(), catalogue(), initialRoles)
	}
}
