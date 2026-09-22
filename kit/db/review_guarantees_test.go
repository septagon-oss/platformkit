package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// These cases pin the guarantees this task's specification named as the ones
// implement had still to write a case for — that ErrContended is the contract
// rather than a sentence, that a budget a deployment named is the budget in
// force, that the runner's two tables are unreachable from an application
// connection, and that a finished drain stays finished when the worker ticks
// again. Each passes on this tree; each is here because nothing else ran it.

// TestAContentionIsErrContendedUnderTheBudgetTheDeploymentNamed.
func TestAContentionIsErrContendedUnderTheBudgetTheDeploymentNamed(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	first := &fstest.MapFile{Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a text)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "probe", Files: fstest.MapFS{
		"000001_probe.up.sql": first,
	}}); err != nil {
		t.Fatal(err)
	}

	// Somebody else holds the lock the next file waits for. An advisory lock is
	// the only wait a case can start on a shared database without stopping a
	// table every parallel case is using, and SQLSTATE 55P03 is the same either
	// way.
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "SELECT pg_advisory_lock($1)", 7240104); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock($1)", 7240104) }()

	files := fstest.MapFS{
		"000001_probe.up.sql": first,
		"000002_wait.up.sql":  {Data: []byte("SELECT pg_advisory_lock(7240104); CREATE TABLE later (x text)")},
	}
	// The budget this run names — not the documented five seconds — and the file
	// sets no budget of its own, so what bounds the wait can only be the value
	// MigrateWith was handed.
	budget := 250 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	started := time.Now()
	err = db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &budget},
		db.MigrationSource{Owner: "probe", Files: files})
	waited := time.Since(started)
	if err == nil {
		t.Fatal("a file whose lock wait exceeded its budget reported success")
	}
	if !errors.Is(err, db.ErrContended) {
		t.Errorf("a contended file returned %q; errors.Is(err, db.ErrContended) is the contract an operator's script reads", err)
	}
	if !strings.Contains(err.Error(), "probe/000002_wait.up.sql") {
		t.Errorf("the refusal did not name the file: %q", err)
	}
	// The 250 ms budget, not the five-second default: a value that reaches the
	// session is a value you can time.
	if waited > 3*time.Second {
		t.Errorf("the wait took %s with a %s lock budget; the budget named by MigrateWith never reached the session", waited.Round(time.Millisecond), budget)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='probe' AND version=2"); n != 0 {
		t.Errorf("a contended file wrote %d history rows; a refusal writes nothing", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname='later' AND relnamespace=current_schema()::regnamespace"); n != 0 {
		t.Errorf("the contended file created %d tables", n)
	}
}

// TestTheRunnerKeepsItsTwoTablesOutOfReachOfTheApplication. kit/db/README.md:
// "Both are revoked from the application role: a ledger an application can edit
// is a release that never happened, and a progress row it can edit is a backfill
// that repeats rows." migrations/rls_test.go says the same of the pair, and names
// this file's package as the test of it. The drain's cursor is the row a tenant's
// connection must not be able to move: nothing else stops a resume that skips
// work or repeats it.
func TestTheRunnerKeepsItsTwoTablesOutOfReachOfTheApplication(t *testing.T) {
	migrateURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,5) g")},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=2
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
	}}); err != nil {
		t.Fatal(err)
	}
	// The drain is finished, so its progress row is gone; the table it lived in
	// is the one under test, and an empty table is still a table the application
	// may not hold.
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 1 {
		t.Fatalf("the drain wrote %d history rows, want 1", n)
	}

	app := dbtest.Open(t, appURL)
	for _, statement := range []string{
		"SELECT count(*) FROM schema_migrations",
		"UPDATE schema_migrations SET version = 99 WHERE owner = 'fill'",
		"SELECT count(*) FROM schema_migration_backfill",
		"INSERT INTO schema_migration_backfill (owner, version, cursor) VALUES ('fill', 2, '999')",
		"UPDATE schema_migration_backfill SET cursor = '999' WHERE owner = 'fill'",
		"DELETE FROM schema_migration_backfill",
	} {
		_, err := app.ExecContext(t.Context(), statement)
		if err == nil {
			t.Errorf("the application role ran %q against the runner's own tables; the REVOKE is the door the docs name and it is shut", firstWords(statement))
			continue
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%q failed with %q, want the privilege refusal rather than a coincidence", firstWords(statement), err)
		}
	}
}

// firstWords is the verb and the table of a statement, which is what an operator
// reads first when one of them was refused.
func firstWords(statement string) string {
	verb, rest, _ := strings.Cut(statement, " ")
	table, _, _ := strings.Cut(rest, " ")
	return verb + " " + table
}

// TestAFinishedDrainIsNotDoneAgainByTheNextTick is the job's idempotency: the work
// is found in the ledger, so a tick after the one that finished it has to find
// nothing and write nothing, and a replay of the version it already applied is the
// one thing the ledger refuses twice.
func TestAFinishedDrainIsNotDoneAgainByTheNextTick(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,25) g")},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)},
	}}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, migrateURL)
	before := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe")
	if before != 25 {
		t.Fatalf("the first drain left sum(passes)=%d, want the one pass over 25 rows", before)
	}
	for tick := range 3 {
		if err := db.Backfill(t.Context(), migrateURL, source); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if after := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe"); after != before {
		t.Errorf("three ticks of the drain job moved the work from %d to %d; an applied version runs again", before, after)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive the drain they belonged to", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 1 {
		t.Errorf("the finished drain holds %d history rows", n)
	}
}

// TestBackfillRefusesADatabaseWithNoLedger is the door's own precondition: it is
// the migration that creates the runner's tables, and a drain that made them
// itself would be DDL outside the advisory lock.
func TestBackfillRefusesADatabaseWithNoLedger(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, migrateURL)
	var tables int
	scan(t, admin, "SELECT count(*) FROM pg_class WHERE relname IN ('schema_migrations','schema_migration_backfill') AND relnamespace=current_schema()::regnamespace", &tables)
	if tables != 0 {
		t.Fatalf("a schema nothing migrated holds %d of the runner's tables", tables)
	}
	err := db.Backfill(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY)")},
	}})
	if err == nil || !strings.Contains(err.Error(), "schema_migrations") {
		t.Fatalf("draining a database with no ledger returned %q; it must refuse and say what runs first", err)
	}
	if created := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname='probe' AND relnamespace=current_schema()::regnamespace"); created != 0 {
		t.Errorf("the refused drain created %d tables; a refusal writes nothing", created)
	}
}

// TestALaterOwnersRefusalStopsTheWholeComposition. readMigrations reads every
// selected source before the pool opens, which is the promise the file's comment
// makes: "an invalid later source must not let an earlier capability change the
// schema". A deployment composes many owners and applies them in one run, so the
// case that matters is the good owner first and the refused one second.
func TestALaterOwnersRefusalStopsTheWholeComposition(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	early := db.MigrationSource{Owner: "early", Files: fstest.MapFS{
		"000001_table.up.sql": {Data: []byte("CREATE TABLE early_rows (id bigint PRIMARY KEY)")},
	}}
	late := db.MigrationSource{Owner: "late", Files: fstest.MapFS{
		"000001_table.up.sql": {Data: []byte("CREATE TABLE late_rows (id bigint PRIMARY KEY, a text)")},
		"000002_drop.up.sql":  {Data: []byte("ALTER TABLE late_rows DROP COLUMN a")},
	}}
	if err := db.Migrate(t.Context(), migrateURL, early, late); err == nil {
		t.Fatal("the composition accepted a file the rule table refuses")
	}
	admin := dbtest.Open(t, migrateURL)
	// The ledger itself is not there either, which is the strongest form of the
	// claim: the run refused before it opened a connection.
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname IN"+
		" ('schema_migrations', 'schema_migration_backfill', 'early_rows', 'late_rows')"+
		" AND relnamespace = current_schema()::regnamespace"); n != 0 {
		t.Errorf("the refused composition left %d tables behind; an invalid later source let an earlier owner change the schema", n)
	}
}

// TestTheInlineDrainStopsAtTheBoundAnInstallationGivesItself. A fresh owner drains
// its own data file during migration, bounded at fifty batches, because a deploy
// step may not stay open for the length of a table. Past the bound the run refuses
// with ErrBackfillBudget and leaves the committed batches and the cursor behind,
// and the next run — which now finds a drain in flight rather than one not yet
// started — resumes at that cursor and finishes. The implementation's own note
// calls this "code that no case reaches".
func TestTheInlineDrainStopsAtTheBoundAnInstallationGivesItself(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,60) g")},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
	}
	source := db.MigrationSource{Owner: "fill", Files: files}
	err := db.Migrate(t.Context(), migrateURL, source)
	if err == nil {
		t.Fatal("an inline drain ran unbounded; the deploy step is open for as long as the table takes")
	}
	if !errors.Is(err, db.ErrBackfillBudget) {
		t.Fatalf("the bound reported %q; errors.Is(err, db.ErrBackfillBudget) is what an operator's script reads", err)
	}
	admin := dbtest.Open(t, migrateURL)
	// The batches that committed stand, and the cursor names where the next run
	// starts. Fifty is the bound the runner gives itself.
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 50 {
		t.Errorf("%d rows were committed at the bound, want the 50 batches it allowed", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 0 {
		t.Errorf("a drain that stopped at its bound wrote %d history rows; it has not finished", n)
	}
	var cursor string
	scan(t, admin, "SELECT cursor FROM schema_migration_backfill WHERE owner='fill' AND version=2", &cursor)
	if cursor != "50" {
		t.Errorf("the bound left cursor %q, want the last key of the last committed batch", cursor)
	}
	if !strings.Contains(err.Error(), "cursor 50") {
		t.Errorf("the refusal %q does not name the cursor the next run restarts from", err)
	}

	// The next run: a drain in flight is the runner's to resume, and the ten rows
	// after the cursor are what it has left.
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the run that resumes the bounded drain: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE NOT done"); n != 0 {
		t.Errorf("%d rows were left behind by the run that resumed the drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 1 {
		t.Errorf("the finished drain holds %d history rows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlived the drain", n)
	}
}

// TestTwoDrainsOfOnePendingFileCommitTheWorkOnce. Two runners over one unfinished
// backfill is the case the cursor's compare-and-set exists for: `Migrate` holds its
// advisory lock for the whole composition and the drain deliberately holds none, so
// `platformkit migrate --drain` beside a worker's tick is two processes writing the
// same rows. The claim is that the loser stops and nothing double-writes; the check
// is the work itself, counted in the rows, not the two runs' messages.
func TestTwoDrainsOfOnePendingFileCommitTheWorkOnce(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,60) g")},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=2
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)},
	}
	source := db.MigrationSource{Owner: "fill", Files: files}
	// The first migration is the release that installed the table on an installed
	// database: the owner has history, so the data file is left for the worker.
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": files["000001_probe.up.sql"],
	}}); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe"); n != 0 {
		t.Fatalf("a deferred data file was drained by the migration: %d rows already marked", n)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- db.Backfill(t.Context(), migrateURL, source)
		}()
	}
	close(start)
	for range 2 {
		err := <-errs
		if err != nil && !strings.Contains(err.Error(), "another runner took the backfill") {
			t.Errorf("a drain over a file two runners were racing returned %q; the only refusal this race may produce is the one that names the other runner", err)
		}
	}
	// Sixty rows, one pass each. One hundred and twenty would be the same work twice.
	if n := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe"); n != 60 {
		t.Errorf("two concurrent drains left sum(passes)=%d, want exactly 60: the sum is the work, and the loser's batch must roll back whole", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1"); n != 0 {
		t.Errorf("%d rows carry %d passes between them: a batch was committed twice or a row was written twice", n, 60)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive the drain: one runner deleted the row the other was still advancing", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 1 {
		t.Errorf("the drain wrote %d history rows, want 1", n)
	}
}
