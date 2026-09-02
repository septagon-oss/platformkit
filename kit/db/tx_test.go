package db_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTenantIsolationIsEnforcedByPostgres is gate 8: it runs as platformkit_app,
// the role row-level security binds, so what it proves is the database's
// behaviour and not the Go code's good intentions.
func TestTenantIsolationIsEnforcedByPostgres(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	createThings(t, ctx, admin)

	acme := newTenant("acme")
	globex := newTenant("globex")
	ctxAcme := tenancy.WithTenant(ctx, acme)
	ctxGlobex := tenancy.WithTenant(ctx, globex)
	token := syscap.NewSystemToken("kit/db test: cross-tenant rollup")

	// Tenant A writes one row.
	if err := db.Run(ctxAcme, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if got := db.TenantOf(tx); got != acme {
			t.Errorf("TenantOf = %v, want %v", got, acme)
		}
		return insert(tx.DB(), acme.ID, "acme-1")
	}); err != nil {
		t.Fatalf("tenant A insert: %v", err)
	}

	// Tenant B cannot see it. There is no WHERE clause anywhere above.
	if n := countAs(t, ctxGlobex, app); n != 0 {
		t.Errorf("tenant B sees %d rows, want 0", n)
	}
	if n := countAs(t, ctxAcme, app); n != 1 {
		t.Errorf("tenant A sees %d rows, want 1", n)
	}

	// A system transaction sees every tenant and may write for either.
	if err := db.RunSystem(ctx, app, token, func(_ context.Context, tx db.Tx[db.System]) error {
		if n := count(t, tx.DB()); n != 1 {
			t.Errorf("system sees %d rows, want 1", n)
		}
		return insert(tx.DB(), globex.ID, "globex-1")
	}); err != nil {
		t.Fatalf("system transaction: %v", err)
	}
	if n := countAs(t, ctxGlobex, app); n != 1 {
		t.Errorf("tenant B sees %d rows after the system write, want 1", n)
	}
	if n := countAs(t, ctxAcme, app); n != 1 {
		t.Errorf("tenant A sees %d rows after the system write, want 1", n)
	}

	// Without a tenant there is nothing to scope to.
	if err := db.Run(ctx, app, noTenantWork); !errors.Is(err, db.ErrNoTenant) {
		t.Errorf("Run without a tenant = %v, want ErrNoTenant", err)
	}

	// The zero token is nil, the one forgery Go allows, and is refused.
	var forged tenancy.SystemToken
	if err := db.RunSystem(ctx, app, forged, noSystemWork); !errors.Is(err, db.ErrNoSystemToken) {
		t.Errorf("RunSystem with a nil token = %v, want ErrNoSystemToken", err)
	}

	// The two scopes do not nest into each other, in either direction, and a
	// second tenant cannot join the first one's transaction.
	err := db.Run(ctxAcme, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		return db.RunSystem(ctx, app, token, noSystemWork)
	})
	if !errors.Is(err, db.ErrScopeMismatch) {
		t.Errorf("RunSystem inside Run = %v, want ErrScopeMismatch", err)
	}
	err = db.RunSystem(ctx, app, token, func(ctx context.Context, _ db.Tx[db.System]) error {
		return db.Run(tenancy.WithTenant(ctx, acme), app, noTenantWork)
	})
	if !errors.Is(err, db.ErrScopeMismatch) {
		t.Errorf("Run inside RunSystem = %v, want ErrScopeMismatch", err)
	}
	err = db.Run(ctxAcme, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		return db.Run(tenancy.WithTenant(ctx, globex), app, noTenantWork)
	})
	if !errors.Is(err, db.ErrScopeMismatch) {
		t.Errorf("a second tenant joining = %v, want ErrScopeMismatch", err)
	}

	// Nesting joins: the inner write is visible to the outer before commit, and
	// the outer's error rolls both back.
	rollback := errors.New("rollback")
	err = db.Run(ctxAcme, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := insert(tx.DB(), acme.ID, "outer"); err != nil {
			return err
		}
		if err := db.Run(ctx, app, func(_ context.Context, inner db.Tx[db.Tenant]) error {
			return insert(inner.DB(), acme.ID, "inner")
		}); err != nil {
			return err
		}
		if n := count(t, tx.DB()); n != 3 {
			t.Errorf("the joined transaction sees %d rows, want 3", n)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Errorf("Run = %v, want the callback's error", err)
	}
	if n := countAs(t, ctxAcme, app); n != 1 {
		t.Errorf("after rollback tenant A sees %d rows, want 1", n)
	}
}

// TestATransactionThatRewritesItsOwnSettingsIsRolledBack.
//
// platformkit.tenant_id and platformkit.system_access are placeholder GUCs, and
// placeholders are USERSET: any statement in a tenant transaction can turn
// system access on, and the database has no privilege to withhold. So the
// runner re-reads both settings before it commits, and a transaction that
// rewrote either one keeps nothing.
func TestATransactionThatRewritesItsOwnSettingsIsRolledBack(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	createThings(t, ctx, admin)

	acme, globex := newTenant("acme"), newTenant("globex")
	ctxAcme := tenancy.WithTenant(ctx, acme)

	err := db.Run(ctxAcme, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		// The escape: one Exec, and the policy stops applying.
		if err := tx.DB().Exec(`SELECT set_config('platformkit.system_access', 'true', true)`).Error; err != nil {
			return err
		}
		if n := count(t, tx.DB()); n != 0 {
			t.Errorf("the escape saw %d rows before it wrote, want 0", n)
		}
		return insert(tx.DB(), globex.ID, "stolen")
	})
	if !errors.Is(err, db.ErrScopeTampered) {
		t.Fatalf("Run = %v, want ErrScopeTampered", err)
	}
	if n := countAs(t, tenancy.WithTenant(ctx, globex), app); n != 0 {
		t.Errorf("the escape's write survived: %d rows, want 0", n)
	}

	// The same in the other direction: a system transaction that pins itself to
	// one tenant is not the transaction the kernel opened either.
	err = db.RunSystem(ctx, app, syscap.NewSystemToken("kit/db test: tamper"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(`SELECT set_config('platformkit.system_access', 'false', true)`).Error
		})
	if !errors.Is(err, db.ErrScopeTampered) {
		t.Errorf("RunSystem = %v, want ErrScopeTampered", err)
	}
}

// TestForceRowLevelSecurityIsWhatBindsTheOwner. ENABLE alone exempts the table's
// owner from its own policy, and the application role owns any table it creates
// itself, so the second ALTER is not decoration.
func TestForceRowLevelSecurityIsWhatBindsTheOwner(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	acme, globex := newTenant("acme"), newTenant("globex")
	role := dbtest.RoleOf(t, mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL"))
	exec(t, ctx, admin, `CREATE TABLE things (id serial PRIMARY KEY, tenant_id uuid NOT NULL, name text NOT NULL)`)
	exec(t, ctx, admin, `ALTER TABLE things OWNER TO `+role)
	exec(t, ctx, admin, `ALTER TABLE things ENABLE ROW LEVEL SECURITY`)
	exec(t, ctx, admin, `CREATE POLICY things_tenant ON things USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))`)

	if err := db.Run(tenancy.WithTenant(ctx, acme), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return insert(tx.DB(), acme.ID, "acme-1")
	}); err != nil {
		t.Fatalf("tenant A insert: %v", err)
	}
	if n := countAs(t, tenancy.WithTenant(ctx, globex), app); n != 1 {
		t.Errorf("with ENABLE only the owner saw %d of another tenant's rows, want the leak (1)", n)
	}

	exec(t, ctx, admin, `ALTER TABLE things FORCE ROW LEVEL SECURITY`)
	if n := countAs(t, tenancy.WithTenant(ctx, globex), app); n != 0 {
		t.Errorf("with FORCE tenant B sees %d rows, want 0", n)
	}
	if n := countAs(t, tenancy.WithTenant(ctx, acme), app); n != 1 {
		t.Errorf("with FORCE tenant A sees %d rows, want 1", n)
	}
}

// TestOpenRefusesSuperuser: a superuser bypasses every policy, so a connection
// as one would make the tests above prove nothing.
func TestOpenRefusesSuperuser(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	c, err := db.Open(t.Context(), adminURL)
	if err == nil {
		_ = c.Close()
		t.Fatal("Open accepted the owner role")
	}
	if role := dbtest.RoleOf(t, adminURL); !strings.Contains(err.Error(), role) {
		t.Errorf("error does not name the role %q: %v", role, err)
	}
}

func newTenant(slug string) tenancy.Tenant {
	return tenancy.Tenant{ID: uuid.New(), Slug: slug, Name: slug}
}

func noTenantWork(context.Context, db.Tx[db.Tenant]) error { return nil }
func noSystemWork(context.Context, db.Tx[db.System]) error { return nil }

func mustEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is unset", name)
	}
	return v
}

// createThings installs one tenant-owned table with the policy shape
// migrations/000001_tenancy.up.sql documents. dbtest.Schema has already applied
// that migration into this test's own schema.
func createThings(t *testing.T, ctx context.Context, admin sqlDB) {
	t.Helper()
	exec(t, ctx, admin, `CREATE TABLE things (id serial PRIMARY KEY, tenant_id uuid NOT NULL, name text NOT NULL)`)
	exec(t, ctx, admin, `ALTER TABLE things ENABLE ROW LEVEL SECURITY`)
	exec(t, ctx, admin, `ALTER TABLE things FORCE ROW LEVEL SECURITY`)
	exec(t, ctx, admin, `CREATE POLICY things_tenant ON things USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))`)
}

func insert(gdb *gorm.DB, tenantID uuid.UUID, name string) error {
	return gdb.Exec("INSERT INTO things (tenant_id, name) VALUES (?, ?)", tenantID.String(), name).Error
}

func count(t *testing.T, gdb *gorm.DB) int {
	t.Helper()
	var n int
	if err := gdb.Raw("SELECT count(*) FROM things").Row().Scan(&n); err != nil {
		t.Fatalf("count things: %v", err)
	}
	return n
}

func countAs(t *testing.T, ctx context.Context, app *db.Conn) int {
	t.Helper()
	var n int
	if err := db.Run(ctx, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		n = count(t, tx.DB())
		return nil
	}); err != nil {
		t.Fatalf("count as tenant: %v", err)
	}
	return n
}
