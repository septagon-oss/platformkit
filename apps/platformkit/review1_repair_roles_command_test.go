package main

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestTheRepairCommandListsBeforeItRemoves drives `platformkit repair-roles`
// itself, which is the deliverable the brief asks for by name and which no case
// in this repository runs: modules/auth proves the module function, and this is
// the only place the fifth subcommand — its configuration, its composition, its
// tenant walk and its two modes — is exercised at all.
//
// The installation is the real bootstrap's. A grant no composed module defines
// is put into the operator tenant's built-in admin role and into a role the
// tenant made, the way an older seeder and an older composition left them, and
// then the command is asked the two questions it exists for: say what is there,
// and take it away. What it may never do is take a grant out of a role its
// seeder never wrote.
//
// The reachability probe is the row, not the command's printing: each step
// reads the roles back through the composed service and asserts on the grant
// list, so the case fails the same way whether the command prints nothing, the
// wrong thing or the right thing about a row it did not change.
func TestTheRepairCommandListsBeforeItRemoves(t *testing.T) {
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

	// ghost:read is the shape of the defect: a permission a module that has
	// left the composition used to define. It goes in by SQL because every
	// door this application has refuses to write one, which is exactly why the
	// rows this command repairs can only have been left by an older seeder.
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if e := tx.DB().Exec(
			"UPDATE roles SET permissions = array_append(permissions, 'ghost:read') WHERE tenant_id = ? AND name = ?",
			acme.ID, authcontracts.RoleAdmin).Error; e != nil {
			return e
		}
		return tx.DB().Exec(
			"INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?)",
			acme.ID, "finance", `{"ghost:read"}`).Error
	})
	if err != nil {
		t.Fatalf("leave the rows an older seeder left: %v", err)
	}

	read := func() map[string][]string {
		t.Helper()
		out := map[string][]string{}
		err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				roles, e := c.auth.Roles(ctx, tx)
				if e != nil {
					return e
				}
				for _, r := range roles {
					out[r.Name] = []string(r.Grants)
				}
				return nil
			})
		if err != nil {
			t.Fatalf("read the roles back: %v", err)
		}
		return out
	}

	// Listing is the default and it is a report: after it the rows are what
	// they were, or the command is the silent edit it exists not to be.
	if err := repairRoles([]string{"--config", path}); err != nil {
		t.Fatalf("repair-roles: %v", err)
	}
	after := read()
	if !slices.Contains(after[authcontracts.RoleAdmin], "ghost:read") {
		t.Errorf("listing removed %q from the admin role: %v", "ghost:read", after[authcontracts.RoleAdmin])
	}
	if !slices.Contains(after["finance"], "ghost:read") {
		t.Errorf("listing removed %q from the tenant's own role: %v", "ghost:read", after["finance"])
	}

	// --remove is the decision, and it reaches only the roles the seeder owns.
	if err := repairRoles([]string{"--config", path, "--remove"}); err != nil {
		t.Fatalf("repair-roles --remove: %v", err)
	}
	after = read()
	if slices.Contains(after[authcontracts.RoleAdmin], "ghost:read") {
		t.Errorf("--remove left %q in the admin role: %v", "ghost:read", after[authcontracts.RoleAdmin])
	}
	if !slices.Contains(after[authcontracts.RoleAdmin], authcontracts.Wildcard) {
		t.Errorf("--remove took the wildcard off the administrator: %v", after[authcontracts.RoleAdmin])
	}
	if !slices.Contains(after["finance"], "ghost:read") {
		t.Errorf("--remove reached into a role the seeder never wrote: %v", after["finance"])
	}

	// Idempotent: a second run finds nothing and changes nothing.
	was := slices.Clone(after[authcontracts.RoleAdmin])
	if err := repairRoles([]string{"--config", path, "--remove"}); err != nil {
		t.Fatalf("second repair-roles --remove: %v", err)
	}
	if again := read()[authcontracts.RoleAdmin]; !slices.Equal(again, was) {
		t.Errorf("the second run changed the admin role from %v to %v", was, again)
	}
}
