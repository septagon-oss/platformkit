package auth

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

// RepairSeededRoles is the door for an installation seeded before a grant was
// only ever as wide as the composition: it reports, per role, every grant in
// the roles this module's seeder owns that no composed module declares, and
// takes them away when remove is true. What it returns is what it found, role
// name to permissions, whether or not it removed anything.
//
// It is a command an operator runs and never a silent edit, which is why the
// two halves are one call with a flag rather than a repair that happens on the
// way past. The rows belong to a customer and were legal when they were
// written; deciding they are not is a person's decision, and the listing is how
// they make it. See apps/platformkit's repair-roles command, which is the
// composition's half: the catalogue and the initial roles it passes here are
// the same two values it passes SeedRoles.
//
// It never touches a tenant's own roles. The names it is allowed to write are
// exactly the ones contracts.SeededRoles returns for this composition — the
// built-in administrator, the built-in member and the application's initial
// roles — so a role a customer created naming a permission that left with its
// module keeps it, and keeps being reported by the hourly sweep, which is the
// only thing that can say what the customer meant by it.
//
// It is idempotent because it is a filter: a second run reads roles that hold
// only declared permissions and finds nothing to do. The removal goes through
// SetRole, so it takes that tenant's role lock, is held to the floor that keeps
// somebody able to administer the tenant, and publishes auth.role_set in the
// same transaction — a repair no audit could see would be the silent edit this
// exists not to be.
func RepairSeededRoles(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.Service,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	return internal.RepairSeededRoles(ctx, tx, roles, declared, defaults, remove)
}
