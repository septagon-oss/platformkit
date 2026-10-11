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
//
// The view dies with the callback. The settings it borrows live on the system
// transaction's connection, and the restore at the end puts cross-tenant access
// back there; a db.Tx[db.Tenant] the hook kept would still be typed for one
// tenant while reading every tenant. So the callback runs under a context this
// call cancels on its way out, and every statement the lent handle carries —
// the handle itself, or one taken from the context inside — fails as soon as the
// callback returns, before reaching the server. Tenancy stays held by the type
// and by the context, never by the caller's restraint.
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
	lentCtx, release := context.WithCancel(tenancy.WithTenant(ctx, tenant))
	defer release()
	// One session of the caller's transaction, bound to the context this call
	// owns. It shares the transaction's connection and its settings, and nothing
	// else: committing, rolling back and restoring the settings stay with the
	// system handle the caller passed in.
	lent := system.db.WithContext(lentCtx)
	tenantCtx := context.WithValue(lentCtx, txKey{}, openTx{db: lent, tenant: tenant})
	if err := fn(tenantCtx, Tx[Tenant]{db: lent, scope: Tenant{tenant: tenant}}); err != nil {
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
