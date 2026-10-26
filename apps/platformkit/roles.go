// roles.go is the list somebody wrote down: who a tenant of this application
// begins as, beside the two roles auth owns.
//
// It used to be derived from the composed manifests. With the modules wiring
// themselves there is no moment when they exist and Roles(…) has not been named, so
// the list is written here and the derivation became a check:
// TestEveryRoleEveryComposedModuleDeclaresIsOneThisApplicationSeeds names the omit.
//
// The admin and member roles are auth's own, and a module that adds two doors of its
// own is answered by those two: admin holds every tenant-scoped permission, and the
// task personas are the task module's, granted to match what it declares.
//
// The operator half of a new administrator's grants is not here either: seedRoles
// reads it off the composed manifests and refuses a permission none defines.
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
