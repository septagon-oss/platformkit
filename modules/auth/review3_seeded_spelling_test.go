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

// Review 3's pin: the one normalisation the repair's answer rests on and nothing
// else in the tree exercises.
//
// contracts.SeededGrants decides who wrote a grant, and for an initial role it
// decides it by matching the row's permissions against the literal the
// application wrote — normalising that literal as it goes
// (strings.ToLower(strings.TrimSpace(named)), roles.go:256) because the row
// holds what CheckedPermissions made of it and not what the literal says. Every
// other case on this branch hands the seeder a literal already in canonical
// spelling, so all of them pass with the normalisation deleted, and a product
// whose literal says " Content:Read " would then keep the dead grant this task
// exists to remove: the repair would find nothing to take, report nothing, and
// the hourly warning would go on for a row the seeder itself wrote.
//
// The same case pins the role name's half of it — the literal is " Finance " and
// the report is keyed "finance" — because SeededGrants looks the role up by
// ValidRoleName of the literal's name, and a lookup that stopped normalising
// would be the same silent nothing.
//
// The reachability probe is in the fixed behaviour and not in the defect's
// output: the row this reads back must still hold role:manage (the grant the
// module that stayed defines), so a repair that did nothing, refused, or emptied
// the role fails here rather than passing. The second run asserts the
// idempotence the exported contract claims.
var (
	spelledWas = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "content:read"},
		{Permission: "tenant:manage", Operator: true}}
	spelledNow = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "tenant:manage", Operator: true}}
	// The application's literal, in the spelling a person writes rather than the
	// one the column holds: a capital, and whitespace around both the name and
	// the permission.
	spelledInitial = []contracts.Role{{Name: " Finance ",
		Grants: contracts.Permissions{" Content:Read ", "role:manage"}}}
)

func TestTheSeederOwnsAGrantItsLiteralSpellsDifferently(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, acme, spelledWas, spelledInitial)
	})
	if err != nil {
		t.Fatalf("seed %s from a literal spelled %q: %v", acme.Slug, spelledInitial[0].Grants, err)
	}
	// What the seeder wrote is the normalised row, which is why the repair has to
	// normalise the literal to recognise its own grant.
	if grants := spelledRoleGrants(t, conn, svc, acme, "finance"); !slices.Equal(grants, []string{"content:read", "role:manage"}) {
		t.Fatalf("the seeder wrote finance holding %v, want [content:read role:manage]; the rest of this case reads that row", grants)
	}

	found := spelledRepair(t, conn, svc, acme, true)
	if !slices.Equal(found["finance"], []string{"content:read"}) {
		t.Errorf("the repair reported %v for a role its own seeder created; the literal spells the grant %q"+
			" and the row holds %q, so a repair that does not normalise the literal leaves the dead grant"+
			" this command exists to take", found, " Content:Read ", "content:read")
	}
	grants := spelledRoleGrants(t, conn, svc, acme, "finance")
	if !slices.Contains(grants, "role:manage") {
		t.Fatalf("finance came out of the repair holding %v: the grant a composed module still defines has to stay,"+
			" and a role the repair emptied would pass the assertion above for the wrong reason", grants)
	}
	if slices.Contains(grants, "content:read") {
		t.Errorf("finance still holds %q after the removal: %v", "content:read", grants)
	}

	// Idempotent, which is what the exported contract says: the second run reads
	// a role holding only declared permissions and finds nothing.
	if again := spelledRepair(t, conn, svc, acme, true); len(again) != 0 {
		t.Errorf("the second repair reported %v; a repair that already ran has nothing left to take", again)
	}
}

func spelledRepair(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant, remove bool) map[string][]string {
	t.Helper()
	var found map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, spelledNow, spelledInitial, remove)
		return e
	})
	if err != nil {
		t.Fatalf("repair %s (remove=%v): %v", tenant.Slug, remove, err)
	}
	return found
}

func spelledRoleGrants(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant, name string) []string {
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

// TestTheRepairReadsOnlyItsOwnTenantsRoles is review 3's second pin, and it
// reads the repair's *read* rather than its write.
//
// internal.RepairSeededRoles lists roles with Service.Roles, which carries no
// tenant predicate at all (roles.go:60, `tx.DB().Order("name").Find(&out)`) and
// is confined by row-level security alone. Review 1's tenancy case proves the
// other half — repairing one tenant leaves another tenant's administrator
// untouched — but both of its tenants hold a role called "admin", so a leaking
// read would land on the same map key and the same row name and be invisible.
// Here the two tenants are seeded from different initial roles, so a leaked row
// has a name of its own: the repair of beta may not report alpha's role, and
// must not create it either, because a row it reports it writes.
//
// The reachability probe is beta's own role, which the same run has to repair.
func TestTheRepairReadsOnlyItsOwnTenantsRoles(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	alpha := tenancy.Tenant{ID: uuid.New(), Slug: "alpha"}
	beta := tenancy.Tenant{ID: uuid.New(), Slug: "beta"}
	onlyAlpha := []contracts.Role{{Name: "onlyalpha", Grants: contracts.Permissions{"content:read", "role:manage"}}}
	onlyBeta := []contracts.Role{{Name: "onlybeta", Grants: contracts.Permissions{"content:read", "role:manage"}}}

	for _, seed := range []struct {
		tenant tenancy.Tenant
		roles  []contracts.Role
	}{{alpha, onlyAlpha}, {beta, onlyBeta}} {
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return auth.SeedRoles(ctx, tx, seed.tenant, spelledWas, seed.roles)
		})
		if err != nil {
			t.Fatalf("seed %s: %v", seed.tenant.Slug, err)
		}
	}

	// The application's literal names both roles, which is what a repair is
	// handed; only one of them is in this tenant.
	defaults := append(slices.Clone(onlyAlpha), onlyBeta...)
	var found map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), beta), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, spelledNow, defaults, true)
		return e
	})
	if err != nil {
		t.Fatalf("repair beta: %v", err)
	}
	if !slices.Equal(found["onlybeta"], []string{"content:read"}) {
		t.Fatalf("the repair of beta reported %v; it was supposed to find the dead grant in its own role", found)
	}
	if grants, ok := found["onlyalpha"]; ok {
		t.Errorf("the repair of beta reported alpha's role onlyalpha (%v): the read that lists a tenant's roles"+
			" carries no tenant predicate and is confined by row-level security alone", grants)
	}
	if grants := spelledRoleGrants(t, conn, svc, beta, "onlyalpha"); grants != nil {
		t.Errorf("the repair of beta wrote a role of alpha's into beta: onlyalpha holds %v there", grants)
	}
	if grants := spelledRoleGrants(t, conn, svc, alpha, "onlyalpha"); !slices.Contains(grants, "content:read") {
		t.Errorf("repairing beta reached into alpha's onlyalpha: %v", grants)
	}
}
