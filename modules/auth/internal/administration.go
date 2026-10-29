package internal

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// AdministeringRoles lists roles that can restore tenant administration.
// It shares the ordinary/operator permission rule with the authorizer.
func AdministeringRoles(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	if err := grantTenant(ctx, tx); err != nil {
		return nil, err
	}
	var roles []*contracts.Role
	if err := tx.DB().WithContext(ctx).Order("name").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("auth: read the roles: %w", err)
	}
	manage := tenancy.Grant{Permission: contracts.PermissionRoleManage}
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		if contracts.Match(role.Grants, manage) {
			out = append(out, role.Name)
		}
	}
	return out, nil
}
