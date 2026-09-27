package internal

import (
	"context"
	"slices"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// RepairSeededRoles is auth.RepairSeededRoles, which is the exported door and
// says what this is for and why the removal is asked for rather than assumed.
//
// It composes three things that already exist and adds no storage path of its
// own: Undeclared for the grants no composed module defines — the same function
// the hourly sweep warns with, so the command and the warning read one list —
// contracts.SeededGrants for which of those this module's seeder is the writer
// of, and SetRole for the write, which holds the lock, the administration floor
// and the event.
//
// It asks contracts nothing that can refuse. contracts.SeededRoles validates the
// initial roles an application names, and the installation this command exists
// for is the one whose literal still names a permission that left with its
// module — the same deploy that left the rows being repaired — so calling it here
// would answer an operator with a refusal instead of the report.
// contracts.SeededGrants reads the same literal and checks nothing.
//
// What it returns is what it removed, or with remove false what it would: the
// seeder's own dead grants, role by role. A role holding a dead grant of
// somebody else's is left out of it whole, because it cannot be written at all —
// contracts.CheckedPermissions refuses a list naming a permission no module
// defines, so taking the seeder's grant out of that role would mean taking the
// other one with it, and that one is not this command's to take. The sweep goes
// on reporting those.
func RepairSeededRoles(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.Service,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	held, err := roles.Roles(ctx, tx)
	if err != nil {
		return nil, err
	}
	tenant := db.TenantOf(tx)
	dead := Undeclared(held, declared)
	found := map[string][]string{}
	for _, r := range held {
		gone := dead[r.Name]
		ours := contracts.SeededGrants(*r, gone, defaults, tenant)
		if len(ours) == 0 || len(ours) != len(gone) {
			continue
		}
		found[r.Name] = ours
	}
	if !remove {
		return found, nil
	}
	for _, r := range held {
		gone, ok := found[r.Name]
		if !ok {
			continue
		}
		keep := slices.DeleteFunc(slices.Clone([]string(r.Grants)), func(p string) bool {
			return slices.Contains(gone, p)
		})
		_, wrote, err := roles.SetRoleChanged(ctx, tx, r.Name, keep, declared)
		if err != nil {
			return nil, err
		}
		if !wrote {
			// The row already held this list by the time this run got the
			// tenant's lock: somebody else took the same grant between the read
			// above and the write here, and asked for the same thing. Nothing
			// about that is wrong — it is what a lock in front of a row is for —
			// but this run wrote no row and publishes no event, so it says it
			// removed nothing. A line saying "removed" is a claim about a row.
			delete(found, r.Name)
		}
	}
	return found, nil
}
