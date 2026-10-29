package main

// The seed command proves an operator before it writes anything, and the grant it
// asks for is tenant:manage — which the tenant module declares an operator
// permission, so the wildcard does not answer it. The ordinary refusal of an
// operator permission is pinned elsewhere; this is the CLI's own door, and the
// shape that matters is the one a working installation can actually reach: a
// person of the installation tenant who holds a role granting everything in the
// tenant, with a password that checks out, and no named operator grant. Such a
// person may not authorise a run that acts on another tenant's rows. The refused
// run writes nothing: the page the plan would have named keeps its title.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

const (
	wildcardEmail     = "wildcard@acme.localhost"
	wildcardPassword  = "a wildcard holder's own long credential"
	wildcardRole      = "universal"
	wildcardUntouched = "Standing while the wildcard is not an operator grant"
)

func TestSeedCommandRefusesAnOperatorWhoseAuthorityIsTheWildcardAlone(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	tenantID, err := func() (tenancy.Tenant, error) {
		var found tenancy.Tenant
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			var e error
			found, e = c.testTenants().ByHost(ctx, system, acmeHost)
			return e
		})
		return found, err
	}()
	if err != nil {
		t.Fatal(err)
	}

	title := func(want string) func(context.Context, db.Tx[db.System]) error {
		return func(ctx context.Context, system db.Tx[db.System]) error {
			return db.InTenant(ctx, system, tenantID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				home, e := c.contents.Public(ctx, tx, "home")
				if e != nil {
					return e
				}
				if want == "" {
					if home.Title != wildcardUntouched {
						t.Errorf("the refused run left the page titled %q; want %q", home.Title, wildcardUntouched)
					}
					return nil
				}
				_, e = content.Spec.UpdateRow(ctx, tx, home.ID, map[string]any{"title": want})
				return e
			})
		}
	}
	if err := dbtest.System(t.Context(), conn, title(wildcardUntouched)); err != nil {
		t.Fatal(err)
	}

	// The person is invited and given a password through the user module, and the
	// role is written as a row because no command may hand out a role that
	// administers the tenant — which is the point: this role grants the wildcard,
	// so it is exactly the one an operator may only name deliberately.
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		return db.InTenant(ctx, system, tenantID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, e := c.testUsers().Invite(ctx, tx, wildcardEmail, "Wildcard holder")
			if e != nil {
				return e
			}
			return c.testUsers().SetPassword(ctx, tx, person.ID, wildcardPassword)
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, system db.Tx[db.System]) error {
		if e := system.DB().Exec(
			`INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ARRAY['*'])`,
			tenantID.ID, wildcardRole).Error; e != nil {
			return e
		}
		return system.DB().Exec(
			`UPDATE users SET roles = ARRAY[?] WHERE tenant_id = ? AND email = ?`,
			wildcardRole, tenantID.ID, wildcardEmail).Error
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", wildcardEmail)
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", wildcardPassword)
	err = seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail})
	switch {
	case err == nil:
		t.Fatal("the seed command ran as a person holding only the wildcard; want it refused")
	case !strings.Contains(err.Error(), tenantcontracts.PermissionTenantManage):
		t.Fatalf("seed command run by the wildcard holder: %q; want it to name %q", err, tenantcontracts.PermissionTenantManage)
	}
	if err := dbtest.System(t.Context(), conn, title("")); err != nil {
		t.Fatal(err)
	}
}
