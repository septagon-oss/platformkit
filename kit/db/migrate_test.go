package db_test

import (
	"context"
	"database/sql"
	"fmt"
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

// TestAnOwnerAdoptsHistoryWithoutReapplyingIt is the upgrade a module goes
// through when it takes its SQL out of the foundation: the old layout applied
// everything as one owner, the new layout names the same files under two, and
// the second run must neither refuse the old owner's now-missing files nor run
// the adopted SQL again — CREATE TABLE would fail, and an INSERT would double.
func TestAnOwnerAdoptsHistoryWithoutReapplyingIt(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	customers := &fstest.MapFile{Data: []byte("CREATE TABLE customers (name text); INSERT INTO customers VALUES ('Alice')")}
	orders := &fstest.MapFile{Data: []byte("CREATE TABLE orders (customer text); INSERT INTO orders VALUES ('Alice')")}
	active := &fstest.MapFile{Data: []byte("ALTER TABLE customers ADD COLUMN active boolean NOT NULL DEFAULT true")}
	before := db.MigrationSource{Owner: "core", Files: fstest.MapFS{
		"000001_customers.up.sql": customers, "000002_orders.up.sql": orders, "000003_active.up.sql": active,
	}}
	if err := db.Migrate(t.Context(), migrateURL, before); err != nil {
		t.Fatal(err)
	}
	// The new layout: core keeps 1 and 3, the orders module owns 2 under the
	// same number and says where it came from.
	core := db.MigrationSource{Owner: "core", Files: fstest.MapFS{"000001_customers.up.sql": customers, "000003_active.up.sql": active}}
	sales := fstest.MapFS{"000002_orders.up.sql": orders}
	after := []db.MigrationSource{core, {Owner: "orders", Files: sales, Adopts: []db.Adoption{{Owner: "core", Versions: []int64{2}}}}}
	for range 2 {
		if err := db.Migrate(t.Context(), migrateURL, after...); err != nil {
			t.Fatalf("upgrade to module-owned history: %v", err)
		}
	}
	admin := dbtest.Open(t, migrateURL)
	var rows int
	var owners string
	scan(t, admin, "SELECT count(*) FROM orders", &rows)
	scan(t, admin, "SELECT string_agg(owner || '/' || version, ',' ORDER BY version) FROM schema_migrations", &owners)
	if rows != 1 || owners != "core/1,orders/2,core/3" {
		t.Fatalf("adoption re-ran SQL or left the ledger wrong: orders=%d ledger=%s", rows, owners)
	}
	// The new owner advances on its own from here.
	sales["000004_paid.up.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE orders ADD COLUMN paid boolean NOT NULL DEFAULT false")}
	if err := db.Migrate(t.Context(), migrateURL, after...); err != nil {
		t.Fatal(err)
	}
	scan(t, admin, "SELECT string_agg(owner || '/' || version, ',' ORDER BY version) FROM schema_migrations", &owners)
	if owners != "core/1,orders/2,core/3,orders/4" {
		t.Fatalf("ledger after the adopting owner's own migration = %s", owners)
	}
	// A fresh installation has nothing to adopt and reads the same ledger.
	freshURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), freshURL, after...); err != nil {
		t.Fatal(err)
	}
	scan(t, dbtest.Open(t, freshURL), "SELECT string_agg(owner || '/' || version, ',' ORDER BY version) FROM schema_migrations", &owners)
	if owners != "core/1,orders/2,core/3,orders/4" {
		t.Fatalf("fresh ledger = %s", owners)
	}
}

// TestAdoptionRefusesAChangedFileAndLeavesHistoryAlone: an adopted row is an
// applied file, and applied files are immutable whoever owns them.
func TestAdoptionRefusesAChangedFileAndLeavesHistoryAlone(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	before := db.MigrationSource{Owner: "core", Files: fstest.MapFS{
		"1_customers.up.sql": {Data: []byte("CREATE TABLE customers (name text)")},
		"2_orders.up.sql":    {Data: []byte("CREATE TABLE orders (customer text)")},
	}}
	if err := db.Migrate(t.Context(), migrateURL, before); err != nil {
		t.Fatal(err)
	}
	after := []db.MigrationSource{
		{Owner: "core", Files: fstest.MapFS{"1_customers.up.sql": {Data: []byte("CREATE TABLE customers (name text)")}}},
		{Owner: "orders", Files: fstest.MapFS{"2_orders.up.sql": {Data: []byte("CREATE TABLE orders (customer text, total int)")}},
			Adopts: []db.Adoption{{Owner: "core", Versions: []int64{2}}}},
	}
	err := db.Migrate(t.Context(), migrateURL, after...)
	if err == nil || !strings.Contains(err.Error(), "orders/2_orders.up.sql was applied as core/2_orders.up.sql with different content") {
		t.Fatalf("changed adopted file = %v", err)
	}
	var owners string
	scan(t, dbtest.Open(t, migrateURL), "SELECT string_agg(owner || '/' || version, ',' ORDER BY version) FROM schema_migrations", &owners)
	if owners != "core/1,core/2" {
		t.Fatalf("a refused adoption changed the ledger: %s", owners)
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

// The patience TestMigrationCancellationRollsBackAndReleasesTheLock asks for, and the
// two states of a migration run that set the terms of it. The case needs the run to be
// inside the SQL it came to run before cancelling it means anything, and the one thing
// standing between a run and its own SQL is the composition key: one advisory lock per
// database (kit/db/migrate.go, compositionLockKey) while the tests of this repository
// get one schema each and not one database each —
// composition_lock_rehold_test.go's header counts them. So a run can spend longer
// reaching `pg_advisory_lock` than anything here spends inside it, and
// `holdCompositionLock` is patient about that on purpose, bounded only by the caller's
// context. A probe that bounds the wall clock bounds that queue with it, and reads the
// queue as the bug: this case answered a run the server could have shown queued for the
// key with "migration never reached its SQL" — the one thing a queued run is not.
const (
	// migrationStillness is how long a run the server shows doing nothing at all —
	// no session of this test's, or one in none of the states below — is given
	// before the probe gives up on it. The window this case has always carried,
	// unchanged: a run that never opens a session is broken whatever the machine is
	// doing, and this is the wait that says so.
	migrationStillness = 10 * time.Second

	// migrationQueueCeiling is the most patience the queue alone can earn. No
	// legitimate queue reaches it — the key comes back when the run holding it
	// finishes what it holds it for — and it exists because the alternative to a
	// ceiling is this case sitting out a session that never gave the key back until
	// the package's own timeout takes the process down with everyone else's.
	migrationQueueCeiling = 3 * time.Minute

	// migrationPoll is how often the probe asks the server, and the smallest unit
	// of queue time it can count.
	migrationPoll = 10 * time.Millisecond

	// migrationKeyGiveBack is how long the cancelled run's session is given to be
	// seen out of the key; see awaitKeyReleased.
	migrationKeyGiveBack = 5 * time.Second
)

// The states migrationRunState reads a run's session in, spelled as pg_stat_activity
// spells them: inside the pg_sleep of the file it came to run, and waiting on the
// composition key.
const (
	migrationInSQL  = "sql"
	migrationQueued = "queued"
)

// migrationRunState answers what the sessions of this connection — this test's own,
// because every URL dbtest returns carries the test's schema as its search_path and as
// its application_name — are doing at this instant: "sql", "queued", "running" for
// anything else it has a live session doing, and "" for none. pg_stat_activity is the
// only account of that instant, and asking it is what separates a run queued for a lock
// from a run that will never reach its SQL, which is the distinction a stopwatch cannot
// make and the one this file got wrong.
func migrationRunState(ctx context.Context, watch sqlDB) (string, error) {
	var state string
	err := watch.QueryRowContext(ctx, `SELECT CASE
			WHEN count(*) FILTER (WHERE wait_event = 'PgSleep') > 0 THEN 'sql'
			WHEN count(*) FILTER (WHERE wait_event_type = 'Lock' AND wait_event = 'advisory') > 0 THEN 'queued'
			WHEN count(*) > 0 THEN 'running' ELSE '' END
			FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid()
				AND state <> 'idle' AND application_name = current_setting('search_path')`).Scan(&state)
	return state, err
}

// watchMigrationRun waits for this test's migration run to reach want, or — when want
// is "" — for over to close, which is a run arriving by returning rather than by
// reaching a state. It cancels the run and answers with why instead, when the session
// has shown nothing at all for stillness or has earned migrationQueueCeiling of pure
// queue: the first is the run that never reached its SQL, which is what the probe
// exists to catch, and the second is a key nobody gave back.
//
// The clock stops while the run is *seen* queued, which is the whole of the change:
// what the caller asks for is patience about the run, and a queue for one advisory lock
// in a database every package of this repository boots a schema inside belongs to the
// machine. over is nil where the run's own return is no success the probe should take —
// a Migrate that came back before its SQL is exactly the failure the stillness names.
// It reads its own connection and returns its reasons rather than failing t, so that
// the one caller that cannot fail the test from where it stands can still watch.
func watchMigrationRun(ctx context.Context, watch sqlDB, want string, over <-chan struct{}, stillness time.Duration, cancel func()) error {
	started, queued := time.Now(), time.Duration(0)
	for {
		select {
		case <-over:
			return nil
		default:
		}
		state, err := migrationRunState(ctx, watch)
		if err != nil {
			cancel()
			return fmt.Errorf("read the run's own session: %w", err)
		}
		if want != "" && state == want {
			return nil
		}
		if state == migrationQueued {
			queued += migrationPoll
			if queued >= migrationQueueCeiling {
				cancel()
				return fmt.Errorf("%s queued for the composition key, the most patience a queue alone earns before this case calls the key stuck", queued.Round(time.Second))
			}
		}
		if time.Since(started) <= stillness+queued {
			time.Sleep(migrationPoll)
			continue
		}
		cancel()
		return fmt.Errorf("no session of this test's seen at work within %s, %s of it queued for the composition key",
			stillness, queued.Round(time.Millisecond))
	}
}

// awaitKeyReleased answers with how many sessions of this test still hold the
// composition key, giving up at migrationKeyGiveBack. This is the claim the case's name
// makes — that a cancelled run releases the lock — read off pg_locks rather than
// inferred from how quickly the retry below happens to get its turn, and read of *this
// test's* sessions rather than of the key anywhere in the database: a sibling package's
// run may hold that at this instant and may hold it legitimately. The wait is the time
// the server takes to end a session whose socket has gone, which is the release path
// when the run's own `pg_advisory_unlock` did not land (kit/db/migrate.go, the defer
// that says so); a session still holding the key past it is the leak this case is for.
func awaitKeyReleased(t *testing.T, admin sqlDB) int {
	t.Helper()
	deadline := time.Now().Add(migrationKeyGiveBack)
	for {
		held := countRows(t, admin, "SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid"+
			" WHERE l.locktype = 'advisory' AND l.objid = 7240101 AND a.datname = current_database()"+
			" AND a.application_name = current_setting('search_path')")
		if held == 0 || time.Now().After(deadline) {
			return held
		}
		time.Sleep(migrationPoll)
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
	if err := watchMigrationRun(t.Context(), admin, migrationInSQL, nil, migrationStillness, cancel); err != nil {
		t.Fatalf("migration never reached its SQL: %v (%v)", <-done, err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("migration ignored cancellation")
	}
	if held := awaitKeyReleased(t, admin); held != 0 {
		t.Fatalf("%d sessions of this test still hold the composition key after the run returned: the cancelled session took it and never gave it back", held)
	}
	files["1_slow.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE slow (value int); INSERT INTO slow VALUES (1)")}
	// The retry, with the same patience up front and the same ceiling under it: it
	// queues for the same key, so five seconds of wall clock bounded the machine's
	// turn rather than the run's, and the release it was standing in for is asserted
	// above now rather than inferred from this.
	retry, retryStop := context.WithCancel(t.Context())
	defer retryStop()
	over := make(chan struct{})
	var retryErr error
	go func() { retryErr = db.Migrate(retry, migrateURL, source); close(over) }()
	gaveUp := make(chan error, 1)
	go func() { gaveUp <- watchMigrationRun(t.Context(), admin, "", over, migrationStillness, retryStop) }()
	<-over
	if why := <-gaveUp; why != nil {
		t.Fatalf("retry after cancellation: %v (%v)", retryErr, why)
	}
	if retryErr != nil {
		t.Fatalf("retry after cancellation: %v", retryErr)
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
		"adopts a version it lacks": {Owner: "adopter", Files: valid.Files,
			Adopts: []db.Adoption{{Owner: "elsewhere", Versions: []int64{7}}}},
		"adopts from itself": {Owner: "adopter", Files: valid.Files,
			Adopts: []db.Adoption{{Owner: "adopter", Versions: []int64{1}}}},
		"adopts a version its previous owner still lists": {Owner: "adopter", Files: valid.Files,
			Adopts: []db.Adoption{{Owner: "valid", Versions: []int64{1}}}},
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
