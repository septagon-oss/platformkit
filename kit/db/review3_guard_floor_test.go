package db_test

// review3_guard_floor_test.go is the third review's cases for what T-0018 says
// about its own guard.
//
// Case 1 is about the floor. migrations/README.md: "A rule cannot be refused on a
// file that is already applied somewhere: … So a source states the first version
// it is guarded from … and the number is one past the highest file the rules
// refuse today … Lowering a floor is a review, not an edit", and the task's
// SPECIFY.md promises a floors test that asserts "no floor is above the owner's
// head". migrations/review_floors_test.go asserts the two directions for the four
// sources that exist; the kernel itself accepts any int64 a source writes, so a
// floor above the head — one that skips versions no installation has seen — is a
// guard switched off for the files that have not been written yet, and nothing
// outside that one test file could notice it.
//
// Case 2 pins the wait the README distinguishes from the budgets:
// "pg_advisory_lock runs before the budgets go on the session, so it waits on the
// caller's context alone … When that context runs out the operator gets a context
// deadline, not ErrContended". Nothing else runs that sentence.

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

// TestASourceCannotDeclareAFloorPastItsOwnHead. The floor exists for files some
// installation already applied, and those are exactly the files that exist: a
// number past the source's highest version therefore guards nothing that is
// history and skips what is coming. The case asks for the value to be refused at
// the door that reads it, or for the pending file to be guarded anyway — either
// remedy answers it — and asserts that a rewrite of the kind `alter-column-type`
// exists for does not reach the database either way.
func TestASourceCannotDeclareAFloorPastItsOwnHead(t *testing.T) {
	head := &fstest.MapFile{Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a text)")}
	rewrite := &fstest.MapFile{Data: []byte("ALTER TABLE probe ALTER COLUMN a TYPE varchar(64)")}
	files := fstest.MapFS{"000001_probe.up.sql": head, "000002_widen.up.sql": rewrite}

	t.Run("guarded, the rewrite is refused", func(t *testing.T) {
		migrateURL, _ := dbtest.URLs(t)
		err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "floorhead", Files: files})
		if err == nil || !strings.Contains(err.Error(), "alter-column-type") {
			t.Fatalf("the same two files with no floor answered %v; the rule table refuses this rewrite, and this leg is the control for the leg below", err)
		}
	})

	// The source whose floor is 50 while its own head is version 2. Nothing under
	// that floor has ever been applied anywhere, because nothing under it exists.
	t.Run("past the head, the guard is still a guard", func(t *testing.T) {
		migrateURL, _ := dbtest.URLs(t)
		err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "floorhead", Files: files, RulesFrom: 50})
		if err == nil {
			admin := dbtest.Open(t, migrateURL)
			t.Fatalf("a source whose RulesFrom (50) is past its own highest version (2) applied a table rewrite the rule table refuses: %d history rows, and the guard was never asked about 000002_widen.up.sql",
				countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'floorhead'"))
		}
		if !strings.Contains(err.Error(), "floorhead") {
			t.Errorf("the refusal %q does not name the source whose floor it is refusing", err)
		}
		// Either honest answer: the grammar refuses the declaration, or the pending
		// file is judged by the rule it cannot escape. A refusal that names neither
		// is an error about something else.
		if !strings.Contains(err.Error(), "RulesFrom") && !strings.Contains(err.Error(), "alter-column-type") {
			t.Errorf("the refusal %q names neither the floor the source declared nor the rule the pending file breaks", err)
		}
		admin := dbtest.Open(t, migrateURL)
		if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'floorhead'"); n != 0 {
			t.Errorf("the refused run left %d history rows behind; a refusal writes nothing", n)
		}
	})

	t.Run("and the floor does not reach the release rule", func(t *testing.T) {
		// The same escape, pointed at the guard the brief is really about: a source
		// whose floor is past its head must not be able to apply a contract half in
		// the release that carries its expand. planOwner does not read RulesFrom,
		// and this is the leg that says so from the outside.
		migrateURL, _ := dbtest.URLs(t)
		if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "orders", Files: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total bigint)")},
		}}); err != nil {
			t.Fatal(err)
		}
		err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "orders", RulesFrom: 50, Files: fstest.MapFS{
			"000001_orders.up.sql":     {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total bigint)")},
			"000002_amount.up.sql":     {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
			"000003_drop_total.up.sql": {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")},
		}})
		if err == nil || !strings.Contains(err.Error(), "waits for") {
			t.Errorf("a source with RulesFrom past its head ran a contract half beside its expand: %v", err)
		}
		if n := countRows(t, dbtest.Open(t, migrateURL),
			"SELECT count(*) FROM pg_attribute WHERE attrelid = 'orders'::regclass AND attname = 'amount_minor'"); n != 0 {
			t.Errorf("the refused release applied part of itself: %d", n)
		}
	})
}

// compositionLockKey is kit/db's own, unexported, constant (kit/db/migrate.go):
// the one lock every migration run of this composition holds for its whole run.
const compositionLockKey = 7240101

// TestTheCompositionLockWaitsOnTheCallersContextNotOnABudget pins the README's
// paragraph "Waiting for the composition lock itself is a different wait, and it
// is left patient". The run that gets the lock second has the first one's applied
// files to read, so it waits; what bounds the wait is the caller's context, and
// the answer it gets back is a deadline, not a contention the operator would
// retry, and not the file budget the run was configured with.
func TestTheCompositionLockWaitsOnTheCallersContextNotOnABudget(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Somebody else's migration is running. Take the same lock it would take, with
	// a bounded wait of our own: this database is shared with every other package's
	// tests, and a case that failed because it lost a race for the lock would say
	// nothing about the behaviour under test.
	deadline := time.Now().Add(60 * time.Second)
	for {
		var got bool
		if err := conn.QueryRowContext(t.Context(), "SELECT pg_try_advisory_lock($1)", compositionLockKey).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the composition advisory lock stayed held for 60s; no run of this case can say anything about waiting for it")
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(t.Context()), "SELECT pg_advisory_unlock($1)", compositionLockKey)
	}()

	lock := 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
	defer cancel()
	started := time.Now()
	err = db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
		db.MigrationSource{Owner: "patient", Files: fstest.MapFS{
			"000001_head.up.sql": {Data: []byte("CREATE TABLE patient (id bigint PRIMARY KEY)")},
		}})
	waited := time.Since(started)
	if err == nil {
		t.Fatal("a migration run that never got the composition lock reported success")
	}
	if errors.Is(err, db.ErrContended) {
		t.Errorf("waiting for the composition lock reported %v; the README says a run that waits gets a context deadline, not a contention it would tell the operator to retry", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting for the composition lock past the caller's context reported %v; the caller's context is what bounds that wait", err)
	}
	// The 100ms file budget went on the session only after the lock, so the run
	// waited the context's two seconds. Half of that would mean the lock itself
	// answered the file budget.
	if waited < time.Second {
		t.Errorf("the run gave up after %s on a 2s context and a 100ms lock budget; the budget reached pg_advisory_lock, which the README says it does not", waited.Round(time.Millisecond))
	}
	// A run that never held the lock never created the ledger either, so the
	// assertion is about the file rather than the table: no history row for it,
	// whether or not the tables exist in this schema yet.
	var ledger string
	if err := dbtest.Open(t, migrateURL).QueryRowContext(t.Context(),
		"SELECT coalesce(to_regclass('schema_migrations')::text, 'absent')").Scan(&ledger); err != nil {
		t.Fatalf("looking for the ledger: %v", err)
	}
	if ledger != "absent" {
		if n := countRows(t, dbtest.Open(t, migrateURL), "SELECT count(*) FROM schema_migrations WHERE owner = 'patient'"); n != 0 {
			t.Errorf("a run that never held the lock applied %d files", n)
		}
	}
}
