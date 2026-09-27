package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestTheBootstrapSeedsWhatThisFileComposes is the application's half of "a
// grant is only ever as wide as the composition".
//
// The module's half is proved without an application in
// modules/auth/seed_composition_test.go: contracts.SeededRoles is what the
// seeder writes, over compositions that differ by one module. The half only
// this package can prove is that the catalogue reaching that seeder is this
// file's own — the closure in modules.go, which is the one join the compiler
// cannot check. mods is filled after the hook that reads it is wired, so a
// composition that never filled it would seed an administrator holding the
// wildcard and no operator permission at all, and every other case in this
// package would still pass: the wildcard is what the ordinary routes ask for,
// and the control-plane cases sign in as the administrator this same bootstrap
// created.
//
// So the tenant here is the real one, created by the real bootstrap through the
// real hook, and what it is compared against is contracts.SeededRoles over
// kit/module.Grants of the modules compose returned.
func TestTheBootstrapSeedsWhatThisFileComposes(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		acme, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	// Only the operator's own tenant is seeded operator grants at all, so a
	// case run against any other tenant would pass on an empty list.
	if !acme.Operator {
		t.Fatalf("%s is not the operator's tenant, so this case would compare two empty lists", acme.Slug)
	}

	var got []*authcontracts.Role
	err = db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var e error
			got, e = c.auth.Roles(ctx, tx)
			return e
		})
	if err != nil {
		t.Fatalf("read the seeded roles back: %v", err)
	}
	slices.SortFunc(got, func(a, b *authcontracts.Role) int { return strings.Compare(a.Name, b.Name) })

	declared := module.Grants(c.modules)
	// An installation whose composition declares no operator permission has
	// nothing for this case to be wrong about, and this one composes the tenant
	// module, so the empty list is the unfilled closure and not a product.
	if len(authcontracts.OperatorGrants(declared)) == 0 {
		t.Fatal("this composition declares no operator permission: either modules.go composes no module" +
			" that defines one, or the catalogue the seeder is given is not the one compose built")
	}
	want, err := authcontracts.SeededRoles(declared, initialRoles, acme)
	if err != nil {
		t.Fatalf("what this composition seeds: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("the bootstrap seeded %d roles and this composition's are %d: %+v", len(got), len(want), got)
	}
	for i, role := range want {
		if got[i].Name != role.Name {
			t.Fatalf("the bootstrap seeded the role %q where this composition's is %q", got[i].Name, role.Name)
		}
		for _, p := range role.Grants {
			if !slices.Contains([]string(got[i].Grants), p) {
				t.Errorf("role %q: this composition grants %q and the seeded role does not", role.Name, p)
			}
		}
		for _, p := range got[i].Grants {
			if !slices.Contains([]string(role.Grants), p) {
				t.Errorf("role %q: the seeded role grants %q and this composition does not", role.Name, p)
			}
		}
	}
}

// TestTheRepairIsGivenTheInitialRolesThisCompositionSeeds holds the one join at
// the repair's own seam that the compiler cannot check: repair-roles must be
// handed the same initial roles the seeding hook is, because the roles that
// seeder owns are the only ones it may touch.
//
// Nothing here could say so until now. The reference application names no initial
// roles — nil is the right list for it — so passing nil at the command's call
// site, which is exactly the mistake a product copying this file makes while its
// literal names roles, left the command repairing nothing and printing the
// all-clear, and every case in this package passed: they plant their dead grants
// in roles the seeder owns whatever the literal says. So this case borrows the
// value for one run. The installation is the one the command exists for, made the
// way an older one was: the module's own SeedRoles over a catalogue that still
// declared the permission that later left, and the literal naming it. Then the
// command runs with this composition's real catalogue, which does not declare it.
// What it takes is the grant the literal still names, and only that: the same
// grant beside it in the built-in member — a role the literal does not name —
// stays, because a name in a row is not the seeder's unless the literal wrote it.
// Pass nil at the repair's call site and the first claim below fails with
// ghost:read still in clerk.
func TestTheRepairIsGivenTheInitialRolesThisCompositionSeeds(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		acme, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	saved := initialRoles
	t.Cleanup(func() { initialRoles = saved })
	initialRoles = []authcontracts.Role{
		{Name: "clerk", Grants: authcontracts.Permissions{"task:read", "ghost:read"}},
	}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		declared := append(slices.Clone(module.Grants(c.modules)), tenancy.Grant{Permission: "ghost:read"})
		if e := auth.SeedRoles(ctx, tx, acme, declared, initialRoles); e != nil {
			return e
		}
		return tx.DB().Exec(
			"UPDATE roles SET permissions = array_append(permissions, 'ghost:read') WHERE tenant_id = ? AND name = ?",
			acme.ID, authcontracts.RoleMember).Error
	})
	if err != nil {
		t.Fatalf("seed the installation the way an older one was seeded: %v", err)
	}

	if err := repairRoles([]string{"--config", path, "--remove"}); err != nil {
		t.Fatalf("repair-roles --remove: %v", err)
	}
	for _, want := range []struct {
		role, grant string
		holds       bool
	}{
		{"clerk", "ghost:read", false},
		{"clerk", "task:read", true},
		{authcontracts.RoleMember, "ghost:read", true},
	} {
		var roles []authcontracts.Role
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Where("tenant_id = ? AND name = ?", acme.ID, want.role).Find(&roles).Error
		})
		if err != nil || len(roles) != 1 {
			t.Fatalf("read role %q: %v (%d rows)", want.role, err, len(roles))
		}
		if got := slices.Contains(roles[0].Grants, want.grant); got != want.holds {
			t.Errorf("role %q holds %q is %v, want %v: the repair is given %v, not what the seeding hook is given",
				want.role, want.grant, got, want.holds, initialRoles)
		}
	}
}
