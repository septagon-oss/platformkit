package db_test

// An autocommit file's statement may be cancelled rather than refused: the run's
// own lock budget is on the session while CREATE INDEX CONCURRENTLY waits for the
// transactions already writing the table, and a wait that runs out leaves the
// half-built index behind under the name it was building. The next send of the same
// statement answers "relation already exists, skipping" — success — and a history
// row written on top of that certifies a unique constraint the database does not
// enforce: every ON CONFLICT clause naming the index answers 42P10 from then on, in
// an installation whose migration ledger says the constraint holds. So the object is
// read back before the file is recorded, repaired once if it cannot be relied on,
// and the file refuses if the repair did not take.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestAWriteThatReliesOnACertifiedIndexFindsIt holds the table one row-writer has
// touched shut for longer than the run's lock budget, lets the build die on that,
// and applies the file again the way an operator would: the second run may not
// report success over an index the first one left invalid.
func TestAWriteThatReliesOnACertifiedIndexFindsIt(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx := t.Context()
	// `note` is the ledger's notification_id and `channel` its channel: the pair the
	// partial index is over, so a duplicate pair is the write that relies on it.
	for _, statement := range []string{
		`CREATE TABLE build_probe (id bigint PRIMARY KEY, note bigint NOT NULL, channel text NOT NULL, sent boolean NOT NULL DEFAULT false)`,
		`INSERT INTO build_probe VALUES (1, 10, 'email', true)`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("the fixture's %s: %v", statement, err)
		}
	}
	source := mapSource("build", `-- pkit: autocommit=true
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS build_probe_sent_once ON build_probe (note, channel) WHERE sent;`)

	// The state the reproduction leaves behind, built here rather than assumed: a
	// row-level writer, and a run whose build runs out of its lock budget.
	gate, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	tx, err := gate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE build_probe SET sent = sent WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	lock := 400 * time.Millisecond
	first := func() error {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return db.MigrateWith(runCtx, adminURL, db.MigrationBudget{LockTimeout: &lock}, source)
	}()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("open the gate again: %v", err)
	}
	if first == nil {
		t.Fatal("the build finished while a row-writer held the table, so the case below proves nothing")
	}
	if errors.Is(first, context.DeadlineExceeded) {
		t.Fatalf("the run waited on the gate to the end rather than refusing at its budget: %v", first)
	}
	if valid, _, applied := indexState(t, admin, ctx); valid || applied != 0 {
		t.Errorf("the cancelled run left the file certified (valid=%v, history rows=%d), want an invalid index and no history row: %v", valid, applied, first)
	}

	// The gate is open and the same file is applied again. The invalid index is
	// still there for `IF NOT EXISTS` to call done, and this is where the failure
	// used to be silent.
	second := func() error {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		return db.MigrateWith(runCtx, adminURL, db.MigrationBudget{}, source)
	}()
	valid, ready, applied := indexState(t, admin, ctx)
	if second != nil {
		t.Fatalf("the second run refused where nothing holds the table any more: %v", second)
	}
	if applied != 1 {
		t.Fatalf("the file the second run reported as applied has %d history rows", applied)
	}
	if !valid {
		t.Errorf("the history row certifies build_probe_sent_once with indisvalid=%v, indisready=%v: every write that relies on it answers 42P10", valid, ready)
	}

	// And the constraint is the point of the file: the duplicate pair is refused by
	// the index, which is the only reason the ON CONFLICT clause can name it.
	if _, err := admin.ExecContext(ctx,
		`INSERT INTO build_probe VALUES (2, 10, 'email', true) ON CONFLICT (note, channel) WHERE sent DO NOTHING`); err != nil {
		t.Errorf("a write that relies on the certified index: %v", err)
	}
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM build_probe WHERE sent`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("the duplicate pair got in: %d sent rows, want the index to have refused it", rows)
	}
}

// indexState reads what the catalog says about the index the autocommit file builds,
// and whether a run certified it.
func indexState(t *testing.T, admin *sql.DB, ctx context.Context) (valid, ready bool, applied int) {
	t.Helper()
	err := admin.QueryRowContext(ctx, `
		SELECT i.indisvalid, i.indisready FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'build_probe_sent_once'`).Scan(&valid, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, 0
	}
	if err != nil {
		t.Fatalf("read the index the file builds: %v", err)
	}
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations WHERE owner = 'build' AND version = 1`).Scan(&applied); err != nil {
		t.Fatalf("read the file's history row: %v", err)
	}
	return valid, ready, applied
}
