package internal

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// SeedRoles installs defaults inside the transaction creating a tenant. The
// application validates custom grants against its manifests before opening the
// database. This function owns the write and protects the built-in admin role;
// repeated provisioning must preserve grants an administrator has since edited.
func SeedRoles(_ context.Context, tx db.Tx[db.System], tenant tenancy.Tenant, operator []string, defaults []contracts.Role) error {
	admin := []string{contracts.Wildcard}
	if tenant.Operator {
		admin = append(admin, operator...)
	}
	roles := map[string][]string{contracts.RoleAdmin: admin, contracts.RoleMember: {}}
	seen := map[string]bool{contracts.RoleAdmin: true}
	for _, role := range defaults {
		name, err := contracts.ValidRoleName(role.Name)
		if err != nil {
			return err
		}
		if seen[name] || len(role.Grants) == 0 {
			return fmt.Errorf("%w: initial role %q is reserved, duplicated or grants nothing", crud.ErrInvalid, name)
		}
		for _, p := range role.Grants {
			if !httpx.ValidPermission(p) || slices.Contains(operator, p) {
				return fmt.Errorf("%w: initial role %q cannot grant %q", crud.ErrInvalid, name, p)
			}
		}
		seen[name] = true
		roles[name] = role.Grants
	}
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		err := tx.DB().Exec(
			"INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			tenant.ID, name, pq.StringArray(roles[name])).Error
		if err != nil {
			return fmt.Errorf("auth: seed the %s role: %w", name, err)
		}
	}
	return nil
}
