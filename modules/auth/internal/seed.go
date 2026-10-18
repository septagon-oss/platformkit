package internal

import (
	"context"
	"fmt"

	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// SeedRoles installs defaults inside the transaction creating a tenant. This
// function owns the write and protects the built-in admin role; repeated
// provisioning must preserve grants an administrator has since edited.
//
// declared is the composition's permission catalogue — kit/module.Grants over
// the modules the application actually composed — and it decides the whole of
// what is written here. It used to be a list of operator permissions the caller
// named, which every product wrote out by hand and no product narrowed when it
// dropped a module: the administrator of an installation without a billing
// module was seeded billing:catalog, which no route would ever accept, and the
// only thing that ever said so was the hourly sweep, an hour later and every
// hour after. See contracts.SeededRoles, which decides; this writes it.
func SeedRoles(_ context.Context, tx db.Tx[db.System], tenant tenancy.Tenant, declared []tenancy.Grant, defaults []contracts.Role) error {
	roles, err := contracts.SeededRoles(declared, defaults, tenant)
	if err != nil {
		return err
	}
	for _, role := range roles {
		err := tx.DB().Exec(
			"INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			tenant.ID, role.Name, pq.StringArray(role.Grants)).Error
		if err != nil {
			return fmt.Errorf("auth: seed the %s role: %w", role.Name, err)
		}
	}
	return nil
}
