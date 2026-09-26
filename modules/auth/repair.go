package auth

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

// RepairSeededRoles is the door for an installation seeded before a grant was
// only ever as wide as the composition: it reports, per role, every grant this
// module's seeder wrote that no composed module declares any more, and takes
// them away when remove is true. What it returns is what it found, role name to
// permissions, whether or not it removed anything.
//
// It is a command an operator runs and never a silent edit, which is why the
// two halves are one call with a flag rather than a repair that happens on the
// way past. The rows belong to a customer and were legal when they were
// written; deciding they are not is a person's decision, and the listing is how
// they make it. See apps/platformkit's repair-roles command, which is the
// composition's half: the catalogue and the initial roles it passes here are
// the same two values it passes SeedRoles.
//
// It takes away a grant the seeder did not write in one row and nowhere else. A
// role's name belonging to the seeder is not the same as a grant in it being the
// seeder's: the built-in member is seeded holding nothing at all, and a customer
// tenant's administrator is seeded the wildcard and nothing else, so an ordinary
// permission sitting in either of them was put there by whoever administers the
// tenant, through SetRole, while the module owning it was still composed.
// contracts.SeededGrants is that rule, role by role, and it is the seeder's own
// decision read backwards. Everything it leaves — a role a customer created, a
// grant a customer added — keeps its permission and keeps being reported by the
// hourly sweep, which is the only thing that can say what its author meant by it.
//
// The one row is the operator's own administrator while it still holds the
// wildcard, which is the only role the seeder writes named permissions into.
// Once a module leaves, neither the row nor the catalogue records whether a
// departed permission was an operator one, so the seeder's grants there and a
// hand's are indistinguishable and every dead grant in that row is taken. Each
// is dead either way and the wildcard beside it goes on granting every ordinary
// permission, so nothing that was granting anything is lost; the listing exists
// so that a person sees the row before the removal happens.
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
