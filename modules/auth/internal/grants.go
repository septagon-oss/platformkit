package internal

import (
	"context"
	"fmt"
	"github.com/lib/pq"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// GrantReader reads current role grants using only the caller's tenant transaction.
type GrantReader struct{}

func grantTenant(ctx context.Context, tx db.Tx[db.Tenant]) error {
	tenant := db.TenantOf(tx)
	if tx.DB() == nil || tenant.ID == [16]byte{} {
		return fmt.Errorf("auth: a live tenant transaction is required to resolve grants")
	}
	if current, ok := tenancy.FromContext(ctx); ok && current.ID != tenant.ID {
		return fmt.Errorf("auth: grant context and transaction tenants differ")
	}
	return nil
}

// Allowed refuses mismatched scope and customer requests for operator authority.
func (r GrantReader) Allowed(ctx context.Context, tx db.Tx[db.Tenant], want tenancy.Grant) (bool, error) {
	if err := grantTenant(ctx, tx); err != nil {
		return false, err
	}
	principal, _ := tenancy.PrincipalFrom(ctx)
	if want.Operator && !db.TenantOf(tx).Operator {
		principal = tenancy.Principal{}
	}
	return contracts.Decide(principal, want, func(roles []string) ([]string, error) {
		return r.Permissions(ctx, tx, roles)
	})
}

// Permissions returns current role grants; empty roles do not require a transaction.
func (GrantReader) Permissions(ctx context.Context, tx db.Tx[db.Tenant], roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	if err := grantTenant(ctx, tx); err != nil {
		return nil, err
	}
	var granted []pq.StringArray
	err := tx.DB().WithContext(ctx).Table("roles").Where("name = ANY(?)", pq.StringArray(roles)).
		Pluck("permissions", &granted).Error
	if err != nil {
		return nil, fmt.Errorf("auth: read the roles: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, list := range granted {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out, nil
}
