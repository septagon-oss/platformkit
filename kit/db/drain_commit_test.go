package db_test

// drain_commit_test.go is the case for the moment a drain ends. Counting the rows was
// how the drain's own cases decided it was over, and then they read the two tables:
// between those two facts used to sit a transaction, because the end of the table was
// measured after the last batch had committed, in a transaction of its own. A run that
// stopped inside that moment had written every row and left the row that says "resume
// me" — a drain that reads as unfinished and is not. The end is now measured in the
// transaction that wrote the work, so there is no moment to observe.
//
// The table is an exact multiple of the batch on purpose. A short last window answers
// the question for itself; it is a full last window that has to look at the next one.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTheLastBatchOfADrainCommitsItsOwnHistoryRow.
func TestTheLastBatchOfADrainCommitsItsOwnHistoryRow(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 20) g")},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
	}}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	// Two windows of ten. The row with the highest key and the history row carry the
	// same writing transaction, so the ledger became "applied" in the commit that
	// finished the work — and two transactions wrote the table, one per batch.
	var same int
	scan(t, admin, `SELECT count(*) FROM schema_migrations m
		WHERE m.owner = 'fill' AND m.version = 2
		  AND m.xmin = (SELECT p.xmin FROM probe p WHERE p.id = (SELECT max(id) FROM probe))`, &same)
	if same != 1 {
		t.Errorf("%d, not 1: the history row is not the last batch's own commit, so a table with every row written can still read as a drain to resume", same)
	}
	if n := countRows(t, admin, "SELECT count(DISTINCT xmin::text) FROM probe"); n != 2 {
		t.Errorf("%d transactions wrote the table, want one per batch", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress row(s) outlived the drain", n)
	}
}
