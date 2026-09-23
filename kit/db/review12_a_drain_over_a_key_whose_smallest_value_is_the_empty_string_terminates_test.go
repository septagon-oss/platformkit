package db_test

// review12_a_drain_over_a_key_whose_smallest_value_is_the_empty_string_terminates_test.go is
// the twelfth round's case for the cursor.
//
// `schema_migration_backfill.cursor` is `text NOT NULL DEFAULT ''`, and `windowBound` turns
// the string it reads back into SQL:
//
//	if from == "" { return "TRUE" }                      // "from the start"
//	return `key > $1::type`
//
// So the empty string is the sentinel for "no window committed yet" — and for a `text` (or
// `varchar`) primary key it is also a *value the column can hold*, the smallest one in every
// collation. A drain whose window tops out at that value writes the cursor to the string it
// started with, takes the same window again, and never advances. The two shipped tables of
// this kernel keyed that way are `tenant_hosts.host text PRIMARY KEY` and
// `platformkit_limits.key text PRIMARY KEY`.
//
// Measured, at HEAD, over four rows whose keys are '', 'b', 'c', 'd', with `batch=1`:
//
//	Migrate  → refusal backfill-exceeds-install-budget … (50 batches of 1, cursor )
//	Backfill → context deadline exceeded after 2s, during which the row '' was written
//	           3428 times and the rows 'b', 'c' and 'd' were written 0 times.
//
// `db.Backfill` names no bound — it is the worker's door, composed as `jobs.BackfillMigrations`
// and ticks every five seconds forever — so on an installation this is one row updated as fast
// as the process can commit, each transaction durable, the version never applying, the release
// never converging. It is the write that takes the last one away, repeatedly, of a row nothing
// is waiting for; and the row the drain *should* be walking is never reached. The case below
// writes a body without a value guard, so the repeated write is what the assertion sees; a
// body that guards itself (`WHERE note = '' AND …`, the shape migrations/README.md's own
// example uses) cannot repeat the write, and the same cursor then spins: an endless run of
// empty transactions over one row, which fails every assertion here but the one about writes.
//
// The assertions are the ones the README makes: the cursor is "the last key the run committed
// — a key, not an offset", the drain is "resumable", and a finished drain leaves no progress
// row. They can pass: a run that distinguishes "no window committed" from "the last window
// committed the empty key" — a nullable cursor, or a flag beside it, or a refusal the moment a
// window repeats — reaches the other three rows and ends.

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADrainOverAKeyWhoseSmallestValueIsTheEmptyStringTerminates(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_hosts.up.sql": {Data: []byte(`CREATE TABLE probe (host text PRIMARY KEY, note text NOT NULL DEFAULT '', writes integer NOT NULL DEFAULT 0);
INSERT INTO probe (host) VALUES (''), ('b'), ('c'), ('d')`)},
		"000002_note.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET note = 'done', writes = writes + 1 WHERE host IN (SELECT host FROM batch)`)},
	}
	source := db.MigrationSource{Owner: "hostfill", Files: files}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Errorf("Migrate over a four-row table with a window of one: %v", err)
	}

	// The worker's door, which bounds nothing: the drain has to finish or say it cannot.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := db.Backfill(ctx, migrateURL, source); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the worker's drain never reached the end of a four-row table; it ticked until its context ran out: %v", err)
		} else {
			t.Errorf("the worker's drain refused: %v", err)
		}
	}

	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 4 {
		t.Errorf("%d of 4 rows drained: the window never moved past the key the cursor cannot spell", n)
	}
	if n := countRows(t, admin, "SELECT coalesce(max(writes), 0) FROM probe"); n != 1 {
		t.Errorf("a row written %d times by one drain: the cursor moved to the value it started at and took the same window again", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'hostfill' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for the data file the drain was asked to finish", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive the drain: every later run will take this one up", n)
	}
}
