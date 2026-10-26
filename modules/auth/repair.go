package auth

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

// RepairSeededRoles reports obsolete seeded grants and optionally removes them.
// Deprecated: use RepairSeededRolesWith with a RoleRepairer.
func RepairSeededRoles(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.Service,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	return RepairSeededRolesWith(ctx, tx, roles, declared, defaults, remove)
}

// RepairSeededRolesWith reports obsolete seeded grants; remove enables writes based
// on a reread under the tenant administration lock. The caller owns the transaction.
func RepairSeededRolesWith(ctx context.Context, tx db.Tx[db.Tenant], roles contracts.RoleRepairer,
	declared []tenancy.Grant, defaults []contracts.Role, remove bool,
) (map[string][]string, error) {
	return internal.RepairSeededRoles(ctx, tx, roles, declared, defaults, remove)
}
