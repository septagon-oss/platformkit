package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// InTenant lends a tenant view of an active system transaction to a provisioning
// hook. The caller still owns the commit. A failed callback leaves the settings
// changed, so even a caller that discards its error cannot commit through
// RunSystem's final scope check.
func InTenant(ctx context.Context, system Tx[System], tenant tenancy.Tenant, fn func(context.Context, Tx[Tenant]) error) error {
	currentTx, ok := current(ctx)
	if !ok || !currentTx.system || currentTx.db != system.db || system.db == nil || tenant.ID == uuid.Nil || fn == nil {
		return ErrScopeMismatch
	}
	if err := sealed(system.db, "", "true"); err != nil {
		return err
	}
	if err := system.db.Exec("SELECT set_config('platformkit.system_access', 'false', true)").Error; err != nil {
		return fmt.Errorf("db: enter tenant scope: %w", err)
	}
	if err := system.db.Exec("SELECT set_config('platformkit.tenant_id', ?, true)", tenant.ID.String()).Error; err != nil {
		return fmt.Errorf("db: set bridged tenant: %w", err)
	}
	tenantCtx := context.WithValue(tenancy.WithTenant(ctx, tenant), txKey{}, openTx{db: system.db, tenant: tenant})
	if err := fn(tenantCtx, Tx[Tenant]{db: system.db, scope: Tenant{tenant: tenant}}); err != nil {
		return err
	}
	if err := sealed(system.db, tenant.ID.String(), "false"); err != nil {
		return err
	}
	if err := system.db.Exec("SELECT set_config('platformkit.tenant_id', '', true)").Error; err != nil {
		return fmt.Errorf("db: restore system tenant: %w", err)
	}
	if err := system.db.Exec("SELECT set_config('platformkit.system_access', 'true', true)").Error; err != nil {
		return fmt.Errorf("db: restore system access: %w", err)
	}
	return sealed(system.db, "", "true")
}
