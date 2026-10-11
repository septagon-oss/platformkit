package main

// A deactivated person cannot sign in (user.CanSignIn), and kit/httpx refuses a
// session whose user is not active. The seed command's operator credential says
// it makes "the same reads kit/httpx makes of a request", so a person the
// installation tenant has deactivated — whose hash and roles are still on the
// row — must not be able to run it either. The run is refused and the page this
// test named first is left as it was.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/content"
)

const (
	formerOperatorEmail    = "former-operator@acme.localhost"
	formerOperatorPassword = "a former operator's own long credential"
	leftByFormerOperator   = "Left standing by the deactivated operator"
)

func TestSeedCommandRefusesADeactivatedOperator(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// A second operator with the administrator's own roles and a password, then
	// deactivated: the hash and the roles stay on the row, the status does not.
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		admin, err := deactivatedOperatorRoles(ctx, c, system)
		if err != nil {
			return err
		}
		// Provisioned the way the bootstrap provisions the first administrator:
		// active, with a password and the administrator's own roles.
		former, err := c.users.Provision(ctx, system, tenant.ID, formerOperatorEmail, "Former operator", formerOperatorPassword, admin)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			gone, err := c.users.Deactivate(ctx, tx, former.ID)
			if err != nil {
				return err
			}
			if gone.CanSignIn() {
				t.Fatalf("the deactivated operator can still sign in (status %q)", gone.Status)
			}
			home, err := c.contents.Public(ctx, tx, "home")
			if err != nil {
				return err
			}
			_, err = content.Spec.UpdateRow(ctx, tx, home.ID, map[string]any{"title": leftByFormerOperator})
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", formerOperatorEmail)
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", formerOperatorPassword)
	if err := seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail}); err == nil {
		t.Errorf("seed command run by a deactivated operator = nil; want the run refused")
	}

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			home, err := c.contents.Public(ctx, tx, "home")
			if err != nil {
				return err
			}
			if home.Title != leftByFormerOperator {
				t.Errorf("a deactivated operator's run retitled the home page %q; want %q", home.Title, leftByFormerOperator)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func deactivatedOperatorRoles(ctx context.Context, c composition, system db.Tx[db.System]) ([]string, error) {
	var roles []string
	tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
	if err != nil {
		return nil, err
	}
	err = db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		admin, err := c.users.ByEmail(ctx, tx, adminEmail)
		if err != nil {
			return err
		}
		roles = []string(admin.Roles)
		return nil
	})
	return roles, err
}
