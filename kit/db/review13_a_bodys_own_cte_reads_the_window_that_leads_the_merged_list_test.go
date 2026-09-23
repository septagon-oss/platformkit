package db_test

// review13_a_bodys_own_cte_reads_the_window_that_leads_the_merged_list_test.go pins the one
// property of the twelfth round's wrapper that no case in the tree asks for.
//
// `windowedBody` merges the kernel's window and the body's own CTE list into one `WITH`, and
// documents why the order matters: "The window leads the merged list, so a body's own CTE may
// read it". `TestADataBodyThatOpensWithItsOwnCTEIsDrainedAsOneStatement` walks two bodies that
// name their rows out of the table and then meet the window in the *update*
// (`… INTERSECT SELECT id FROM batch`), so both pass with the window written last as well —
// only the sentence would then be false, and the body that reads the window from inside its
// own list, which is the shape the sentence is about, would answer `relation "batch" does not
// exist`.
//
// This is the pin for that ordering: a body whose named list is defined *from the window*,
// drained over three windows, every even row written once and no odd row written at all.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestABodysOwnCTEReadsTheWindowThatLeadsTheMergedList(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT 'fresh');
INSERT INTO probe (id, note) SELECT g, CASE WHEN g % 2 = 0 THEN 'stale' ELSE 'fresh' END FROM generate_series(1, 12) g`)},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
WITH stale AS (SELECT p.id FROM batch b JOIN probe p ON p.id = b.id WHERE p.note = 'stale')
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM stale)`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "ctehead", Files: files}); err != nil {
		t.Fatalf("a body whose own list reads the window: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 6 {
		t.Errorf("%d of the 6 stale rows drained: the body named them out of the window, which only works if the window leads the merged list", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'fresh'"); n != 6 {
		t.Errorf("%d of the 6 rows the body named away were written", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'ctehead' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for a drain that ran over three windows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive a drain that ran to the end of the table", n)
	}
}
