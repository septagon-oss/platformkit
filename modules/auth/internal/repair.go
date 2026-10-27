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
// Which read decides the write is what this command gets right, and the one that
// decides it is the locked one. Every list it writes is a row with the dead
// grants deleted, so a row that moved between the read and the write makes the
// list wrong in both directions: it revokes a permission somebody had just
// granted, and it hands back one they had just taken away — and only the second
// of those is invisible in what the command prints. readUnderRoleLock is where
// that read happens; it is not a second opinion on the first, it is the read
// whose answer is written.
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
func RepairSeededRoles(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.RoleRepairer,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	held, err := roles.Roles(ctx, tx)
	if err != nil {
		return nil, err
	}
	tenant := db.TenantOf(tx)
	dead := Undeclared(held, declared)
	if remove && len(dead) > 0 {
		// The unlocked read above says only that this tenant has something to take.
		// What is taken is computed from the roles as they stand under the lock.
		if held, err = readUnderRoleLock(ctx, tx, roles, tenant); err != nil {
			return nil, err
		}
		dead = Undeclared(held, declared)
	}
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
			// The report is built from what the write answered and not from what this
			// run subtracted, which is the only way a line saying "removed" is a claim
			// about a row. A run that moved no row and published no event says it
			// removed nothing, even about a grant it did read.
			delete(found, r.Name)
		}
	}
	return found, nil
}

// readUnderRoleLock reads this tenant's roles while holding the role lock its
// writes take, so from this read to the commit that follows it no other
// transaction can move these rows and what a caller subtracts from them is what
// it writes. Asking is not free: this tenant's administrators queue behind
// whoever holds the lock, which is why a caller reads unlocked first and comes
// here only when it has something to write — a listing, and a walk over an
// installation that still declares everything it seeded, take no lock at all.
func readUnderRoleLock(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.RoleRepairer,
	tenant tenancy.Tenant,
) ([]*contracts.Role, error) {
	if err := lockTenantRoles(ctx, tx, tenant); err != nil {
		return nil, err
	}
	return roles.Roles(ctx, tx)
}
