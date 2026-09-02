package db

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/migrations"
)

// TestMigrateIsIdempotent: every replica calls Migrate at boot, so applying an
// already-applied set has to be a no-op rather than an error.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := t.Context()
	admin, _ := TestSchema(t)
	migrateURL := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")

	for i := range 2 {
		if err := Migrate(ctx, migrateURL, migrations.FS); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	var ledgers int
	scan(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'schema_migrations'`, &ledgers)
	if ledgers != 1 {
		t.Errorf("%d schema_migrations tables, want 1", ledgers)
	}
	var (
		version int
		dirty   bool
	)
	scan(t, admin, `SELECT version, dirty FROM schema_migrations`, &version, &dirty)
	if version != 1 || dirty {
		t.Errorf("ledger at version %d dirty=%v, want 1 and clean", version, dirty)
	}
}

// TestTenantHelperFailsClosedOnGarbage: current_setting returns whatever text
// was placed on the transaction, so the helper has to fail closed rather than
// raise. A raising helper would turn a bad setting into a 500 on every query
// instead of into an empty result.
func TestTenantHelperFailsClosedOnGarbage(t *testing.T) {
	ctx := t.Context()
	admin, _ := TestSchema(t)
	if err := Migrate(ctx, mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL"), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := admin.Exec(ctx, `SELECT set_config('platformkit.tenant_id', 'not-a-uuid', false)`); err != nil {
		t.Fatalf("set garbage: %v", err)
	}
	var got sql.NullString
	scan(t, admin, `SELECT platformkit_current_tenant_id()::text`, &got)
	if got.Valid {
		t.Errorf("platformkit_current_tenant_id() = %q, want NULL", got.String)
	}

	// A valid setting still comes back, so the guard is not simply refusing.
	want := uuid.New()
	if err := admin.Exec(ctx, `SELECT set_config('platformkit.tenant_id', $1, false)`, want.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	scan(t, admin, `SELECT platformkit_current_tenant_id()::text`, &got)
	if got.String != want.String() {
		t.Errorf("platformkit_current_tenant_id() = %q, want %q", got.String, want)
	}

	// system_access is off unless RunSystem turned it on.
	var system bool
	scan(t, admin, `SELECT platformkit_is_system()`, &system)
	if system {
		t.Error("platformkit_is_system() is true outside a system transaction")
	}
}

// scan reads one row through a connection's pool. It reaches into Conn because
// only tests need to query outside a scoped transaction.
func scan(t *testing.T, c *Conn, query string, dest ...any) {
	t.Helper()
	if err := c.db.Raw(query).Row().Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}
