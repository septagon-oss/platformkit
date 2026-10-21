package main

// The operator credential is the only thing that says who is standing at the
// terminal; `--as` only says whose rows the writes name. The permission it has
// to prove is the one the command is actually exercising — acting on a tenant
// other than the operator's own — and the person is proven where that
// permission lives: the installation tenant.
// TestSeedCommandRequiresAnAuthenticatedOperator covers the credential that is
// absent. This covers the one that is present and says the wrong thing twice
// over: the address of a person the installation tenant gives observer and
// nothing else, whose password is correct, which is the whole point, and the
// right address with a password no hash matches. Both runs are the real
// command against a page this test named first, read back in a fresh
// transaction: a run that cannot name its operator writes nothing at all.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/content"
)

const (
	clerkEmail    = "clerk@acme.localhost"
	clerkPassword = "an observer's own long credential"
	untouched     = "Left standing by the refused operator"
)

func TestSeedCommandRefusesAnOperatorWhoCannotManageTenants(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	nameThePage := func(title string) func(context.Context, db.Tx[db.System]) error {
		return func(ctx context.Context, system db.Tx[db.System]) error {
			tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
			if err != nil {
				return err
			}
			return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				home, err := c.contents.Public(ctx, tx, "home")
				if err != nil {
					return err
				}
				if title == "" {
					if home.Title != untouched {
						t.Errorf("refused runs left the page titled %q; want %q", home.Title, untouched)
					}
					return nil
				}
				_, err = content.Spec.UpdateRow(ctx, tx, home.ID, map[string]any{"title": title})
				return err
			})
		}
	}
	if err := dbtest.System(t.Context(), conn, nameThePage(untouched)); err != nil {
		t.Fatal(err)
	}
	// An observer of the installation tenant, with a credential of their own:
	// the person is real, the password is right, and the grant is missing.
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			clerk, err := c.users.Invite(ctx, tx, clerkEmail, "Clerk")
			if err != nil {
				return err
			}
			if _, err := c.users.SetRoles(ctx, tx, clerk.ID, []string{"observer"}); err != nil {
				return err
			}
			return c.users.SetPassword(ctx, tx, clerk.ID, clerkPassword)
		})
	}); err != nil {
		t.Fatal(err)
	}

	runs := []struct {
		name, email, password, want string
	}{
		{"no tenant:manage", clerkEmail, clerkPassword, "tenant:manage"},
		{"a password that matches nobody", adminEmail, "not the administrator's password", "matches nobody"},
	}
	for _, run := range runs {
		t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", run.email)
		t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", run.password)
		err := seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail})
		switch {
		case err == nil:
			t.Errorf("seed command run as %s = nil; want the run refused", run.name)
		case !strings.Contains(err.Error(), run.want):
			t.Errorf("seed command run as %s: %q; want it to name %q", run.name, err, run.want)
		}
		if err := dbtest.System(t.Context(), conn, nameThePage("")); err != nil {
			t.Fatal(err)
		}
	}
}
