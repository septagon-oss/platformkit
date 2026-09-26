package auth_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// The two catalogues of review 2's cases: what the older seeder was handed, and
// the same installation after the modules owning task:read, content:read and
// billing:catalog left it.
var (
	narrowWas = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "task:read"},
		{Permission: "content:read"},
		{Permission: "tenant:manage", Operator: true}, {Permission: "billing:catalog", Operator: true}}
	narrowNow = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "tenant:manage", Operator: true}}
	// The application's initial roles, which the seeder is handed and the repair
	// is handed again. role:manage survives the modules leaving, so the role can
	// be written back without emptying it.
	narrowInitial = []contracts.Role{{Name: "finance", Grants: contracts.Permissions{"content:read", "role:manage"}}}
)

// TestTheRepairLeavesTheAdministratorOfACustomersTenant pins the first of the
// two branches that make review 1's finding 1 cured, and it is the branch with
// nothing else standing on it.
//
// contracts.SeededGrants answers "who wrote this grant", and for the built-in
// administrator it answers "the seeder" only in the operator's own tenant. A
// customer's administrator is seeded the wildcard and nothing else — operator
// permissions are refused there — so every named permission in that row was put
// there by whoever administers the tenant, through SetRole, while the module
// owning it was still composed. When that module leaves, the grant is dead and
// it is still theirs: the hourly sweep names it and this command does not take
// it.
//
// The reachability probe is the application's own initial role, whose dead grant
// the same run does remove, so the case cannot pass by the repair doing nothing.
func TestTheRepairLeavesTheAdministratorOfACustomersTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

	seedForRepair(t, conn, acme)
	handEdit(t, conn, svc, acme, contracts.RoleAdmin, []string{contracts.Wildcard, "task:read"})

	found := repair(t, conn, svc, acme, true)
	if !slices.Equal(found["finance"], []string{"content:read"}) {
		t.Fatalf("the repair reported %v; it was supposed to find the grant its own seeder wrote into finance", found)
	}
	if grants := roleGrants(t, conn, svc, acme, "finance"); !slices.Equal(grants, []string{"role:manage"}) {
		t.Fatalf("finance came out of the repair holding %v, want [role:manage]", grants)
	}
	if _, touched := found[contracts.RoleAdmin]; touched {
		t.Errorf("the repair reported the administrator of a customer's tenant: %v", found)
	}
	if grants := roleGrants(t, conn, svc, acme, contracts.RoleAdmin); !slices.Contains(grants, "task:read") {
		t.Errorf("the repair took %q out of %s's administrator: %v. A customer's administrator is seeded"+
			" the wildcard and nothing else, so a named permission in it is somebody's decision"+
			" and not a grant the seeder created", "task:read", acme.Slug, grants)
	}
}

// TestTheRepairLeavesAnAdministratorRowThatLostItsWildcard pins the second
// branch, and the reasoning it rests on.
//
// In the operator's own tenant the seeder writes the wildcard beside the
// catalogue's operator permissions, and the argument for taking a dead grant
// back out of that row is that beside a wildcard an ordinary name adds nothing,
// so what a name can add there is an operator permission. The argument holds
// only while the wildcard is in the row. Once somebody has taken it off, the
// names that are left are theirs and each one means something, so none of them
// is the command's to remove.
//
// The reachability probe is again the application's initial role, repaired in
// the same run.
func TestTheRepairLeavesAnAdministratorRowThatLostItsWildcard(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}

	seedForRepair(t, conn, operator)
	// What an administrator did while every module was still composed: named the
	// grants out instead of holding the wildcard. role:manage stays, so the floor
	// that keeps somebody able to administer the tenant is satisfied.
	handEdit(t, conn, svc, operator, contracts.RoleAdmin,
		[]string{"role:manage", "task:read", "tenant:manage", "billing:catalog"})

	found := repair(t, conn, svc, operator, true)
	if !slices.Equal(found["finance"], []string{"content:read"}) {
		t.Fatalf("the repair reported %v; it was supposed to find the grant its own seeder wrote into finance", found)
	}
	if _, touched := found[contracts.RoleAdmin]; touched {
		t.Errorf("the repair reported an administrator row that no longer holds the wildcard: %v", found)
	}
	grants := roleGrants(t, conn, svc, operator, contracts.RoleAdmin)
	for _, p := range []string{"billing:catalog", "task:read"} {
		if !slices.Contains(grants, p) {
			t.Errorf("the repair took %q out of an administrator row that holds no wildcard: %v."+
				" Every name left in that row grants something, so none of them is the seeder's to take back", p, grants)
		}
	}
}

func seedForRepair(t *testing.T, conn *db.Conn, tenant tenancy.Tenant) {
	t.Helper()
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, narrowWas, narrowInitial)
	})
	if err != nil {
		t.Fatalf("seed %s: %v", tenant.Slug, err)
	}
}

func handEdit(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant, name string, want []string) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, e := svc.SetRole(ctx, tx, name, want, narrowWas)
		return e
	})
	if err != nil {
		t.Fatalf("the administrator's own edit of %s: %v", name, err)
	}
}

func repair(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant, remove bool) map[string][]string {
	t.Helper()
	var found map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, narrowNow, narrowInitial, remove)
		return e
	})
	if err != nil {
		t.Fatalf("repair %s (remove=%v): %v", tenant.Slug, remove, err)
	}
	return found
}

func roleGrants(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant, name string) []string {
	t.Helper()
	var out []string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, e := svc.Roles(ctx, tx)
		if e != nil {
			return e
		}
		for _, r := range roles {
			if r.Name == name {
				out = []string(r.Grants)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s's %s role: %v", tenant.Slug, name, err)
	}
	return out
}
