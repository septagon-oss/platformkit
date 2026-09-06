package auth_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestRoleProvisioningNeedsOnlyItsTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for _, operator := range []bool{false, true} {
		tenant := tenancy.Tenant{ID: uuid.New(), Operator: operator}
		defaults := []contracts.Role{{Name: "member", Grants: contracts.Permissions{"task:read"}}}
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if err := auth.SeedRoles(ctx, tx, tenant, []string{"tenant:manage"}, defaults); err != nil {
				return err
			}
			var roles []contracts.Role
			if err := tx.DB().Where("tenant_id = ?", tenant.ID).Order("name").Find(&roles).Error; err != nil {
				return err
			}
			admin := contracts.Permissions{"*"}
			if operator {
				admin = append(admin, "tenant:manage")
			}
			if len(roles) != 2 || roles[0].Name != "admin" || !reflect.DeepEqual(roles[0].Grants, admin) ||
				roles[1].Name != "member" || !reflect.DeepEqual(roles[1].Grants, contracts.Permissions{"task:read"}) {
				t.Fatalf("operator=%v: unexpected initial roles: %+v", operator, roles)
			}
			if err := tx.DB().Exec("UPDATE roles SET permissions = '{}' WHERE tenant_id = ?", tenant.ID).Error; err != nil {
				return err
			}
			if err := auth.SeedRoles(ctx, tx, tenant, []string{"tenant:manage"}, defaults); err != nil {
				return err
			}
			var changed int64
			if err := tx.DB().Table("roles").Where("tenant_id = ? AND cardinality(permissions) <> 0", tenant.ID).Count(&changed).Error; err != nil {
				return err
			}
			if changed != 0 {
				t.Fatal("repeated provisioning overwrote edited grants")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitialRoleRefusalsAndRollbackLeaveNoRoles(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New()}
	for _, role := range []contracts.Role{
		{Name: "admin", Grants: contracts.Permissions{"task:read"}},
		{Name: "bad-role"},
		{Name: "empty"},
		{Name: "member", Grants: contracts.Permissions{"*"}},
		{Name: "member", Grants: contracts.Permissions{"tenant:manage"}},
	} {
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return auth.SeedRoles(ctx, tx, tenant, []string{"tenant:manage"}, []contracts.Role{role})
		})
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("initial role %+v: %v, want ErrInvalid", role, err)
		}
	}
	rollback := errors.New("tenant creation failed")
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := auth.SeedRoles(ctx, tx, tenant, nil, nil); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM roles WHERE tenant_id = $1", tenant.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("refused or rolled-back provisioning left %d roles", rows)
	}
}
