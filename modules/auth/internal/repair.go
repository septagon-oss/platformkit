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
// own: contracts.SeededRoles for the names this module's seeder owns, Undeclared
// for which of their grants no composed module defines — the same function the
// hourly sweep reports with, so the command repairs exactly what the warning
// names — and SetRole for the write, which holds the lock, the administration
// floor and the event.
func RepairSeededRoles(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.Service,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	seeded, err := contracts.SeededRoles(declared, defaults, db.TenantOf(tx))
	if err != nil {
		return nil, err
	}
	held, err := roles.Roles(ctx, tx)
	if err != nil {
		return nil, err
	}
	owned := make([]*contracts.Role, 0, len(seeded))
	for _, r := range held {
		if slices.ContainsFunc(seeded, func(s contracts.Role) bool { return s.Name == r.Name }) {
			owned = append(owned, r)
		}
	}
	found := Undeclared(owned, declared)
	if !remove {
		return found, nil
	}
	for _, r := range owned {
		gone, ok := found[r.Name]
		if !ok {
			continue
		}
		keep := slices.DeleteFunc(slices.Clone([]string(r.Grants)), func(p string) bool {
			return slices.Contains(gone, p)
		})
		if _, err := roles.SetRole(ctx, tx, r.Name, keep, declared); err != nil {
			return nil, err
		}
	}
	return found, nil
}
