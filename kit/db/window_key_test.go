package db_test

// window_key_test.go is the delivery's case for what a window may ask of a key.
//
// The drain asks the server for the ordering (`ORDER BY key LIMIT n`) and always did; what
// it used to ask for the top of the window was `max(key)`, and PostgreSQL has no `max`
// aggregate for `uuid` or for `bytea` — which is every entity table this kernel ships, from
// `tenants` and `users` down to each module's own, and the type `modules/auth` keys its
// token hashes by. The top is now taken the way the window is (`ORDER BY key DESC LIMIT 1`),
// so what the drain can window over stays the set of types with an ordering operator. The
// review's `review12_a_data_file_is_drained_over_a_key_the_server_can_order_but_not_maximum_test.go`
// walks the first window of a uuid and a bytea table; what follows is the half that needs
// two runs to see, and the half about the ledger row in between them:
//
//   - the rendered key has to survive the round trip, because the cursor is stored as text
//     and comes back as `$1::uuid` — a drain that stops at its batch bound and resumes is
//     the run that proves the rendering, and a rendering that lost precision or byte order
//     repeats rows or skips them at the seam rather than in the first window;
//   - the row a drain leaves when it dies before its first commit holds no key at all, and
//     for a table keyed by `text` there is no string that can say so: the empty string is a
//     key, and the smallest one. That row has to mean "from the start", including over the
//     row keyed by the empty string itself.

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestADrainOverAKeyOfItsOwnRendersTheCursorTheNextRunReadsBack puts a uuid-keyed table
// past the fifty batches a migration drains for itself, so the run stops at its bound with
// a cursor behind it, and then asks the next run to finish the work. The resumed run
// restarts at a key it read out of the ledger as text and compares as `$1::uuid`: every row
// drained exactly once, one history row, no progress row.
func TestADrainOverAKeyOfItsOwnRendersTheCursorTheNextRunReadsBack(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT gen_random_uuid() FROM generate_series(1, 60) g`)},
		"000002_mark.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)},
	}
	source := db.MigrationSource{Owner: "uuidkey", Files: files}
	err := db.Migrate(t.Context(), migrateURL, source)
	if err == nil || !errors.Is(err, db.ErrBackfillBudget) {
		t.Fatalf("a sixty-row table with a window of one stopped at %v; the install bound is what puts a cursor in the ledger to resume from", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written twice by the first run", n)
	}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the run that resumes a uuid-keyed drain: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes = 1"); n != 60 {
		t.Errorf("%d of 60 rows drained: the cursor the first run left did not come back as the same key", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written again across the seam between the two runs", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'uuidkey' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for one drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive a drain that finished", n)
	}
}

// TestADrainResumesFromARowThatHoldsNoKeyYet is the empty-string key, from the side the
// ledger takes. The progress row below is the one a killed run leaves: `beginDrain`
// committed it, no window did. For a table keyed by `text` the cursor column cannot hold
// "nothing committed" as a value — ” is `tenant_hosts.host`'s smallest possible row — so it
// holds a NULL, and the first window has to take the whole table, the empty key included.
func TestADrainResumesFromARowThatHoldsNoKeyYet(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_hosts.up.sql": {Data: []byte(`CREATE TABLE probe (host text PRIMARY KEY, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (host) VALUES (''), ('b'), ('c'), ('d')`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "nullkey", Files: files}); err != nil {
		t.Fatal(err)
	}
	files["000002_mark.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE host IN (SELECT host FROM batch)`)}
	source := db.MigrationSource{Owner: "nullkey", Files: files}
	admin := dbtest.Open(t, migrateURL)
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO schema_migration_backfill (owner, version) VALUES ('nullkey', 2)"); err != nil {
		t.Fatalf("the row a run leaves when it dies before its first window: %v", err)
	}
	// Bounded by hand: db.Backfill names no bound of its own (it is the worker's door,
	// and a tick that stops early has the next tick behind it), and a cursor that cannot
	// tell the empty key from "nothing yet" takes the same window again forever.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := db.Backfill(ctx, migrateURL, source); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the drain that resumes from no key at all never reached the end of a four-row table: %v", err)
		}
		t.Fatalf("the drain that resumes from no key at all: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes = 1"); n != 4 {
		t.Errorf("%d of 4 rows drained: the row keyed by the empty string is a key, and a cursor that cannot tell it from 'nothing yet' stops at it", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written more than once by one drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'nullkey' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for the drain that resumed from a NULL cursor", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive the drain that started from one", n)
	}
}
