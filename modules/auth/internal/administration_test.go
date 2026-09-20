package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestAdministeringRolesIsWhatTheUserModuleAsks.
//
// The user module refuses to take the last administrator's roles away and
// cannot tell who an administrator is: who holds a role is its table, what a
// role grants is this one's. auth.AdministeringRoles is the answer it is handed,
// so this is the case that says the answer comes from contracts.Grants — the
// same function the kernel authorizes with — rather than from a second copy of
// that rule written in SQL.
//
// What it pins is the wildcard: admin grants role:manage by holding "*" and
// nothing else, and a SQL predicate looking for the permission by name would
// miss it. It does not pin the operator exception and cannot, because
// AdministeringRoles asks about tenancy.Grant{Permission: PermissionRoleManage}
// with Operator false — role:manage is an ordinary permission, deliberately, so
// the operator branch of Grants is never taken here. Worth knowing which way
// that cuts: if role:manage ever became an operator permission, the wildcard
// would stop granting it and every customer tenant's administrators would lose
// administration at once.
func TestAdministeringRolesIsWhatTheUserModuleAsks(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// The roles a tenant actually has: admin holds the wildcard because
		// SeedRoles gives it one, an operations role names the permission
		// outright, and the other two are ordinary.
		for name, grants := range map[string][]string{
			"admin":   {contracts.Wildcard},
			"ops":     {contracts.PermissionRoleManage},
			"member":  {},
			"support": {"user:read"},
		} {
			err := tx.DB().Exec(
				"INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?)",
				acme.ID, name, contracts.Permissions(grants)).Error
			if err != nil {
				return err
			}
		}
		got, err := auth.AdministeringRoles(ctx, tx)
		if err != nil {
			return err
		}
		if want := []string{"admin", "ops"}; !slices.Equal(got, want) {
			t.Errorf("AdministeringRoles = %v, want %v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the case's transaction: %v", err)
	}

	// A tenant with no roles at all answers nobody rather than an error. That
	// is the state the user module's floor deliberately allows a write in:
	// refusing there would take away the repair as well as the damage.
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := auth.AdministeringRoles(ctx, tx)
		if err != nil {
			return err
		}
		if len(got) != 0 {
			t.Errorf("a tenant with no roles answers %v, want nobody", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the empty tenant's transaction: %v", err)
	}
}
