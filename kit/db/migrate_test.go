package db_test

import (
	"context"
	"database/sql"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// TestMigrateIsIdempotent: every replica calls Migrate at boot, so applying an
// already-applied set has to be a no-op rather than an error.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := t.Context()
	// The ledger is what this test is about, so it counts in the test's own
	// schema: pg_tables is database-wide, and a second ledger anywhere would
	// otherwise be indistinguishable from two here.
	migrateURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, migrateURL)

	for i := range 2 {
		if err := db.Migrate(ctx, migrateURL, migrations.FS); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	var ledgers int
	scan(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'schema_migrations' AND schemaname = current_schema()`, &ledgers)
	if ledgers != 1 {
		t.Errorf("%d schema_migrations tables, want 1", ledgers)
	}
	var (
		version int
		dirty   bool
	)
	scan(t, admin, `SELECT version, dirty FROM schema_migrations`, &version, &dirty)
	if want := latest(t); version != want || dirty {
		t.Errorf("ledger at version %d dirty=%v, want %d and clean", version, dirty, want)
	}
}

// latest is the highest version in migrations/, so the ledger assertion above
// says "everything is applied" rather than a number that goes stale with the
// next migration.
func latest(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations/: %v", err)
	}
	highest := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, err := strconv.Atoi(strings.TrimLeft(strings.SplitN(e.Name(), "_", 2)[0], "0"))
		if err != nil {
			t.Fatalf("%s has no version: %v", e.Name(), err)
		}
		highest = max(highest, v)
	}
	return highest
}

// TestTenantHelperFailsClosedOnGarbage: current_setting returns whatever text
// was placed on the transaction, so the helper has to fail closed rather than
// raise. A raising helper would turn a bad setting into a 500 on every query
// instead of into an empty result.
func TestTenantHelperFailsClosedOnGarbage(t *testing.T) {
	ctx := t.Context()
	pool, _ := dbtest.Schema(t)
	// set_config(..., false) is session-scoped, so the write and the reads have
	// to happen on one connection rather than on whichever the pool hands out.
	admin, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("pin a connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	exec(t, ctx, admin, `SELECT set_config('platformkit.tenant_id', 'not-a-uuid', false)`)
	var got sql.NullString
	scan(t, admin, `SELECT platformkit_current_tenant_id()::text`, &got)
	if got.Valid {
		t.Errorf("platformkit_current_tenant_id() = %q, want NULL", got.String)
	}

	// A valid setting still comes back, so the guard is not simply refusing.
	want := uuid.New()
	exec(t, ctx, admin, `SELECT set_config('platformkit.tenant_id', '`+want.String()+`', false)`)
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

// sqlDB is what both *sql.DB and *sql.Conn offer a test: statements, and one
// row back. A test that needs its statements on one connection asks for a
// *sql.Conn and nothing else changes.
type sqlDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func scan(t *testing.T, admin sqlDB, query string, dest ...any) {
	t.Helper()
	if err := admin.QueryRowContext(t.Context(), query).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

func exec(t *testing.T, ctx context.Context, admin sqlDB, query string) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
