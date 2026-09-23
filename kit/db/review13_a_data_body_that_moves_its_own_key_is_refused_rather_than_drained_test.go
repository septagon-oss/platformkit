package db_test

// review13_a_data_body_that_moves_its_own_key_is_refused_rather_than_drained_test.go is the
// thirteenth round's case for the one thing the drain's cursor model quietly assumes.
//
// The cursor is a key, and the whole resumability claim rests on where that key sits still:
// `kit/db/backfill.go` says progress is "the last key the run committed — a key, not an
// offset", `drainEnds` concludes a window is the last one because "fewer rows than the limit
// means nothing is left above that key", and every batch case in this package ends by asking
// how many rows carry the value twice. All three are true of a body that writes *other*
// columns. A body that writes the key the window runs over — `UPDATE probe SET id = … WHERE
// id IN (SELECT id FROM batch)`, which is what re-keying a table looks like when somebody
// backfills it — moves its own rows back above the cursor, so the table is never empty of
// work, the cursor keeps advancing over rows that are not the ones it committed, and each row
// is written again on every window that finds it.
//
// `Migrate` stops at its fifty-batch bound and reports `ErrBackfillBudget`; the worker does
// not. `Backfill` is documented to drain "unbounded because nothing waits on its boot", so
// one tick of `jobs.BackfillMigrations` is the run that never ends, re-keying the same rows
// upward forever while holding the advisory lock that job's name elects on.
//
// The correct behaviour asserted here is the one the drain already practises two statements
// earlier: a body the window cannot bound is refused before the progress row exists, by name
// (that is what `data-body-unbounded` and the two-statement refusal are for). What the guard
// table cannot see — which column this table's key is — is the fact `drain` already read out
// of the catalog before it wrote anything.
//
// The reachability probe is the run itself: the case bounds the worker with a context, so the
// defect answers with a deadline and the fix answers with a refusal.

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADataBodyThatMovesItsOwnKeyIsRefusedRatherThanDrainedForever(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');
INSERT INTO probe (id) SELECT g FROM generate_series(1, 12) g`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "rekey", Files: files}); err != nil {
		t.Fatal(err)
	}
	files["000002_rekey.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
UPDATE probe SET id = id + 1000, note = 'rekeyed' WHERE id IN (SELECT id FROM batch)`)}
	source := db.MigrationSource{Owner: "rekey", Files: files}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := db.Backfill(ctx, migrateURL, source)
	admin := dbtest.Open(t, migrateURL)
	// Twelve rows, each moved once, put the highest key at 1012 or below; anything above
	// 12000 has been re-keyed at least twice, which is the once-per-row the cursor exists
	// to guarantee, read off the table rather than off a message.
	moved := countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 12000")
	total := countRows(t, admin, "SELECT count(*) FROM probe")
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the worker's drain never reaches the end of a twelve-row table whose body moves the key the window runs over: it re-keys rows an earlier window already committed (%d rows carry a key moved more than once, %d rows in the table) and every tick repeats the work (%v)", moved, total, err)
	}
	if err == nil {
		t.Errorf("the drain reported success over a table it was still rewriting; the version cannot be applied while rows sit above the cursor")
	}
	if moved != 0 {
		t.Errorf("%d rows carry a key moved more than once: the cursor advanced over rows this body had put back above it, and each window wrote rows an earlier window had already written", moved)
	}
	if total != 12 {
		t.Errorf("%d rows in the table after the drain: a body the window cannot bound has no right to leave the table changed beyond one pass", total)
	}
}
