package db_test

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestMigrateIsIdempotent(t *testing.T) {
	migrateURL, appURL := dbtest.URLs(t)
	for range 2 {
		if err := db.Migrate(t.Context(), migrateURL, migrations.Source); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := fs.ReadDir(migrations.Source.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, migrateURL)
	var applied int
	scan(t, admin, `SELECT count(*) FROM schema_migrations WHERE owner = 'platformkit'`, &applied)
	if applied != len(entries) {
		t.Fatalf("applied %d files, want %d", applied, len(entries))
	}
	application := dbtest.Open(t, appURL)
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "TRIGGER"} {
		var allowed bool
		if err := application.QueryRowContext(t.Context(),
			"SELECT has_table_privilege(current_user, 'schema_migrations', $1)", privilege).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Errorf("application role has %s on migration history", privilege)
		}
	}
}

func TestMigrationOwnersAdvanceIndependently(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	core := fstest.MapFS{
		"000001_customers.up.sql": {Data: []byte("CREATE TABLE customers (name text); INSERT INTO customers VALUES ('Alice')")},
	}
	client := fstest.MapFS{
		"002003_orders.up.sql": {Data: []byte("CREATE TABLE orders (customer text); INSERT INTO orders VALUES ('Alice')")},
	}
	sources := []db.MigrationSource{{Owner: "core", Files: core}, {Owner: "client", Files: client}}
	if err := db.Migrate(t.Context(), migrateURL, sources...); err != nil {
		t.Fatal(err)
	}
	// Both revisions are below the client's 2003. Neither may be skipped.
	core["000023_customers_active.up.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE customers ADD COLUMN active boolean NOT NULL DEFAULT true")}
	cart := db.MigrationSource{Owner: "cart", Files: fstest.MapFS{
		"000001_carts.up.sql": {Data: []byte("CREATE TABLE carts (customer text)")},
	}}
	sources = []db.MigrationSource{sources[0], cart, sources[1]}
	if err := db.Migrate(t.Context(), migrateURL, sources...); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, migrateURL)
	var customer, order string
	var active bool
	scan(t, admin, "SELECT name, active FROM customers", &customer, &active)
	scan(t, admin, "SELECT customer FROM orders", &order)
	if customer != "Alice" || order != "Alice" || !active {
		t.Fatalf("upgrade lost data or skipped a change: customer=%q order=%q active=%v", customer, order, active)
	}
	exec(t, t.Context(), admin, "INSERT INTO carts VALUES ('Alice')")
	// Disabling a module leaves data and history intact; re-enabling is a no-op.
	if err := db.Migrate(t.Context(), migrateURL, sources[0]); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), migrateURL, sources...); err != nil {
		t.Fatal(err)
	}
	var carts, applied int
	scan(t, admin, "SELECT count(*) FROM carts", &carts)
	scan(t, admin, "SELECT count(*) FROM schema_migrations", &applied)
	if carts != 1 || applied != 4 {
		t.Fatalf("re-enable: carts=%d applied=%d, want 1 and 4", carts, applied)
	}
}

func TestFailedMigrationRollsBackAndCanBeRetried(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"1_orders.up.sql": {Data: []byte("CREATE TABLE orders (amount int); INSERT INTO orders VALUES (42)")},
		"2_paid.up.sql":   {Data: []byte("ALTER TABLE orders ADD COLUMN paid boolean; SELECT missing_column FROM orders")},
	}
	source := db.MigrationSource{Owner: "sales", Files: files}
	for range 2 {
		err := db.Migrate(t.Context(), migrateURL, source)
		if err == nil || !strings.Contains(err.Error(), "sales/2_paid.up.sql") {
			t.Fatalf("migration = %v, want the failed file", err)
		}
	}
	admin := dbtest.Open(t, migrateURL)
	var applied, columns, amount int
	scan(t, admin, "SELECT count(*) FROM schema_migrations", &applied)
	scan(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'orders' AND column_name = 'paid'", &columns)
	scan(t, admin, "SELECT amount FROM orders", &amount)
	if applied != 1 || columns != 0 || amount != 42 {
		t.Fatalf("failed SQL escaped rollback: history=%d paid columns=%d amount=%d", applied, columns, amount)
	}
	files["2_paid.up.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE orders ADD COLUMN paid boolean NOT NULL DEFAULT false")}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatal(err)
	}
	var paid bool
	scan(t, admin, "SELECT amount, paid FROM orders", &amount, &paid)
	scan(t, admin, "SELECT count(*) FROM schema_migrations", &applied)
	if amount != 42 || paid || applied != 2 {
		t.Fatalf("retry: amount=%d paid=%v applied=%d", amount, paid, applied)
	}
}

func TestMigrationHistoryIsAppendOnly(t *testing.T) {
	for _, change := range []string{"modified", "renamed", "removed", "inserted before"} {
		t.Run(change, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"1_first.up.sql": {Data: []byte("CREATE TABLE steps (step int); INSERT INTO steps VALUES (1)")},
				"10_last.up.sql": {Data: []byte("INSERT INTO steps VALUES (10)")},
			}
			source := db.MigrationSource{Owner: "workflow", Files: files}
			if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "modified":
				files["1_first.up.sql"] = &fstest.MapFile{Data: []byte("SELECT 1")}
			case "renamed":
				files["1_renamed.up.sql"] = files["1_first.up.sql"]
				delete(files, "1_first.up.sql")
			case "removed":
				delete(files, "1_first.up.sql")
			case "inserted before":
				files["2_inserted.up.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO steps VALUES (2)")}
			}
			// Validation of a later owner happens before any pending SQL.
			earlier := db.MigrationSource{Owner: "earlier", Files: fstest.MapFS{
				"1_earlier.up.sql": {Data: []byte("INSERT INTO steps VALUES (99)")},
			}}
			err := db.Migrate(t.Context(), migrateURL, earlier, source)
			if err == nil || !strings.Contains(err.Error(), "workflow/") {
				t.Fatalf("changed history accepted: %v", err)
			}
			var steps string
			scan(t, dbtest.Open(t, migrateURL), "SELECT string_agg(step::text, ',' ORDER BY step) FROM steps", &steps)
			if steps != "1,10" {
				t.Fatalf("history refusal changed state: %s", steps)
			}
		})
	}
}

func TestConcurrentMigrationsApplyEachFileOnce(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := db.MigrationSource{Owner: "workflow", Files: fstest.MapFS{
		"1_first.up.sql":  {Data: []byte("CREATE TABLE steps (step int); INSERT INTO steps VALUES (1)")},
		"10_last.up.sql":  {Data: []byte("INSERT INTO steps VALUES (10)")},
		"2_middle.up.sql": {Data: []byte("INSERT INTO steps VALUES (2)")},
	}}
	start, done := make(chan struct{}), make(chan error, 8)
	for range 8 {
		go func() { <-start; done <- db.Migrate(t.Context(), migrateURL, source) }()
	}
	close(start)
	for range 8 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	var steps string
	scan(t, dbtest.Open(t, migrateURL), "SELECT string_agg(step::text, ',' ORDER BY ctid) FROM steps", &steps)
	if steps != "1,2,10" {
		t.Fatalf("applied steps = %s, want 1,2,10", steps)
	}
}

func TestMigrationCancellationRollsBackAndReleasesTheLock(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"1_slow.up.sql": {Data: []byte("CREATE TABLE slow (value int); SELECT pg_sleep(30)")},
	}
	source := db.MigrationSource{Owner: "slow", Files: files}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- db.Migrate(ctx, migrateURL, source) }()
	admin := dbtest.Open(t, migrateURL)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var sleeping bool
		scan(t, admin, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path') AND wait_event = 'PgSleep')`, &sleeping)
		if sleeping {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("migration never reached its SQL: %v", <-done)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("migration ignored cancellation")
	}
	files["1_slow.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE slow (value int); INSERT INTO slow VALUES (1)")}
	retry, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if err := db.Migrate(retry, migrateURL, source); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
	var value int
	scan(t, dbtest.Open(t, migrateURL), "SELECT value FROM slow", &value)
	if value != 1 {
		t.Fatalf("retry wrote %d, want 1", value)
	}
}

func TestInvalidMigrationSourcesFailBeforeConnecting(t *testing.T) {
	valid := db.MigrationSource{Owner: "valid", Files: fstest.MapFS{"1_ok.up.sql": {Data: []byte("SELECT 1")}}}
	cases := map[string]db.MigrationSource{
		"repeated owner":    valid,
		"invalid owner":     {Owner: "bad owner", Files: valid.Files},
		"nil files":         {Owner: "nil"},
		"nested files":      {Owner: "nested", Files: fstest.MapFS{"migrations/1_x.up.sql": {Data: []byte("SELECT 1")}}},
		"empty directory":   {Owner: "empty", Files: fstest.MapFS{}},
		"empty SQL":         {Owner: "empty", Files: fstest.MapFS{"1_empty.up.sql": {}}},
		"duplicate version": {Owner: "duplicate", Files: fstest.MapFS{"1_a.up.sql": {Data: []byte("SELECT 1")}, "000001_b.up.sql": {Data: []byte("SELECT 1")}}},
		"zero version":      {Owner: "zero", Files: fstest.MapFS{"0_a.up.sql": {Data: []byte("SELECT 1")}}},
		"overflow version":  {Owner: "overflow", Files: fstest.MapFS{"9223372036854775808_a.up.sql": {Data: []byte("SELECT 1")}}},
		"bad filename":      {Owner: "bad", Files: fstest.MapFS{"schema.sql": {Data: []byte("SELECT 1")}}},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			err := db.Migrate(t.Context(), "postgres://invalid", valid, invalid)
			if err == nil || strings.Contains(err.Error(), "connect:") || !strings.Contains(err.Error(), invalid.Owner) {
				t.Fatalf("source validation = %v", err)
			}
		})
	}
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
