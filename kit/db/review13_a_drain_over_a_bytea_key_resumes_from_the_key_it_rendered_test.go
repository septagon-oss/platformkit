package db_test

// review13_a_drain_over_a_bytea_key_resumes_from_the_key_it_rendered_test.go is the
// thirteenth round's pin of the rendering half of the twelfth round's first fix.
//
// The fix took the top of a window off the missing `max` aggregate and onto the key's own
// ordering, and the cursor that travels with it is the key rendered by `w::text` — so a
// drain that stops at its bound and resumes is the run that proves the rendering round
// trips. `kit/db/window_key_test.go` walks that seam for a `uuid` key (60 rows, `batch=1`),
// and `review12_a_data_file_is_drained_over_a_key_the_server_can_order_but_not_maximum_test.go`
// walks a `bytea` table's *first* window only. The type `modules/auth` keys its token hashes
// by is the one this package ships whose text rendering is not its own letters (`\x`-hex),
// and it is the one whose seam nobody has crossed.
//
// Sixty rows, a window of one, so the first run stops at the fifty-batch bound with a
// rendered key in the ledger and the second run restarts from the string it reads back as
// `$1::bytea`: every row written once, one history row, no progress row.

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADrainOverAByteaKeyResumesFromTheKeyItRendered(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (k bytea PRIMARY KEY, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (k) SELECT decode(md5(g::text), 'hex') FROM generate_series(1, 60) g`)},
		"000002_mark.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE k IN (SELECT k FROM batch)`)},
	}
	source := db.MigrationSource{Owner: "byteakey", Files: files}
	if err := db.Migrate(t.Context(), migrateURL, source); !errors.Is(err, db.ErrBackfillBudget) {
		t.Fatalf("a sixty-row bytea table with a window of one stopped at %v; the bound is what puts a rendered key in the ledger", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written twice by the first run", n)
	}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the run that resumes a bytea-keyed drain: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes = 1"); n != 60 {
		t.Errorf("%d of 60 rows drained: the key the first run rendered did not come back as the same key", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written again across the seam between the two runs", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'byteakey' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for one drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive a drain that finished", n)
	}
}
