package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

// TestTenantIsolationIsEnforcedByPostgres is gate 8: it runs as platformkit_app,
// the role row-level security binds, so what it proves is the database's
// behaviour and not the Go code's good intentions.
func TestTenantIsolationIsEnforcedByPostgres(t *testing.T) {
	ctx := t.Context()
	admin, app := TestSchema(t)
	createThings(t, ctx, admin)

	acme := newTenant("acme")
	globex := newTenant("globex")
	ctxAcme := tenancy.WithTenant(ctx, acme)
	ctxGlobex := tenancy.WithTenant(ctx, globex)
	token := syscap.NewSystemToken("kit/db test: cross-tenant rollup")

	// Tenant A writes one row.
	if err := Run(ctxAcme, app, func(_ context.Context, tx Tx[Tenant]) error {
		if got := TenantOf(tx); got != acme {
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
	if err := RunSystem(ctx, app, token, func(_ context.Context, tx Tx[System]) error {
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
	if err := Run(ctx, app, noTenantWork); !errors.Is(err, ErrNoTenant) {
		t.Errorf("Run without a tenant = %v, want ErrNoTenant", err)
	}

	// A token nothing minted carries no reason, and is refused.
	if err := RunSystem(ctx, app, tenancy.SystemToken{}, noSystemWork); !errors.Is(err, ErrNoSystemToken) {
		t.Errorf("RunSystem with a forged token = %v, want ErrNoSystemToken", err)
	}

	// The two scopes do not nest into each other, in either direction, and a
	// second tenant cannot join the first one's transaction.
	err := Run(ctxAcme, app, func(ctx context.Context, _ Tx[Tenant]) error {
		return RunSystem(ctx, app, token, noSystemWork)
	})
	if !errors.Is(err, ErrScopeMismatch) {
		t.Errorf("RunSystem inside Run = %v, want ErrScopeMismatch", err)
	}
	err = RunSystem(ctx, app, token, func(ctx context.Context, _ Tx[System]) error {
		return Run(tenancy.WithTenant(ctx, acme), app, noTenantWork)
	})
	if !errors.Is(err, ErrScopeMismatch) {
		t.Errorf("Run inside RunSystem = %v, want ErrScopeMismatch", err)
	}
	err = Run(ctxAcme, app, func(ctx context.Context, _ Tx[Tenant]) error {
		return Run(tenancy.WithTenant(ctx, globex), app, noTenantWork)
	})
	if !errors.Is(err, ErrScopeMismatch) {
		t.Errorf("a second tenant joining = %v, want ErrScopeMismatch", err)
	}

	// The owner connection is not an application connection.
	if err := Run(ctxAcme, admin, noTenantWork); err == nil {
		t.Error("Run accepted the owner connection")
	}

	// Nesting joins: the inner write is visible to the outer before commit, and
	// the outer's error rolls both back.
	rollback := errors.New("rollback")
	err = Run(ctxAcme, app, func(ctx context.Context, tx Tx[Tenant]) error {
		if err := insert(tx.DB(), acme.ID, "outer"); err != nil {
			return err
		}
		if err := Run(ctx, app, func(_ context.Context, inner Tx[Tenant]) error {
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

// TestOpenRefusesSuperuser: a superuser bypasses every policy, so a connection
// as one would make the test above prove nothing.
func TestOpenRefusesSuperuser(t *testing.T) {
	adminURL := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	c, err := Open(t.Context(), adminURL)
	if err == nil {
		_ = c.Close()
		t.Fatal("Open accepted the owner role")
	}
	if role := roleOf(t, adminURL); !strings.Contains(err.Error(), role) {
		t.Errorf("error does not name the role %q: %v", role, err)
	}
}

func newTenant(slug string) tenancy.Tenant {
	return tenancy.Tenant{ID: uuid.New(), Slug: slug, Name: slug}
}

func noTenantWork(context.Context, Tx[Tenant]) error { return nil }
func noSystemWork(context.Context, Tx[System]) error { return nil }

// createThings installs the tenancy helpers and one tenant-owned table with the
// policy shape migrations/000001_tenancy.up.sql documents.
func createThings(t *testing.T, ctx context.Context, admin *Conn) {
	t.Helper()
	if err := Migrate(ctx, mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL"), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE things (id serial PRIMARY KEY, tenant_id uuid NOT NULL, name text NOT NULL)`,
		`ALTER TABLE things ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE things FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY things_tenant ON things
			USING (platformkit_tenant_match(tenant_id))
			WITH CHECK (platformkit_tenant_match(tenant_id))`,
	} {
		if err := admin.Exec(ctx, stmt); err != nil {
			t.Fatalf("create things: %v", err)
		}
	}
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

func countAs(t *testing.T, ctx context.Context, app *Conn) int {
	t.Helper()
	var n int
	if err := Run(ctx, app, func(_ context.Context, tx Tx[Tenant]) error {
		n = count(t, tx.DB())
		return nil
	}); err != nil {
		t.Fatalf("count as tenant: %v", err)
	}
	return n
}
