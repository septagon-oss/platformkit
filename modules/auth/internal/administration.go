package internal

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// AdministeringRoles is which of this tenant's roles grant
// contracts.PermissionRoleManage. See auth.AdministeringRoles, which is the
// exported door and says why this is a function rather than a method.
//
// The filter is contracts.Grants and not a SQL predicate, deliberately. The
// wildcard grants every ordinary permission and no operator one, and that rule
// belongs in one place — writing "'role:manage' = ANY(permissions) OR '*' =
// ANY(permissions)" here would be a second copy of it, in a language where the
// first copy cannot be seen from.
func AdministeringRoles(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	var roles []*contracts.Role
	if err := tx.DB().WithContext(ctx).Order("name").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("auth: read the roles: %w", err)
	}
	manage := tenancy.Grant{Permission: contracts.PermissionRoleManage}
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		if contracts.Grants(role.Grants, manage) {
			out = append(out, role.Name)
		}
	}
	return out, nil
}
