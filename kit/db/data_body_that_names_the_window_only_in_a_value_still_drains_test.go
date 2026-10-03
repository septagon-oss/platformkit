package db_test

// The window's guard reads a *binding*, so the word `batch` in text the server runs as data
// binds nothing and the file still drains. Two shapes of that, because the guard makes its
// judgement on the body with the literals put away: a value that spells the whole binding,
// `batch AS (…)`, written by a body that also carries a CTE list of its own; and a value that
// spells the name alone, written by a body with no list at all. The first is the one that would
// refuse a correct file, because a guard matching text sees `batch as (` there and the wrapper
// has a real second CTE to join; the second is the same claim with nothing else in the body to
// confuse it. Both drain every row, and neither leaves a resumable progress row behind — the
// state that would tell a later run a drain started that never did.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestABodyThatNamesTheWindowOnlyInAValueStillDrainsUnderTheGuard(t *testing.T) {
	for _, tc := range []struct{ name, body, countValue string }{
		{
			name: "a value that spells the whole binding, beside a CTE list of its own",
			body: `WITH stale (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'batch AS (the word in a value)' WHERE id IN (SELECT id FROM batch)`,
			countValue: "SELECT count(*) FROM probe WHERE note LIKE 'batch AS (%'",
		},
		{
			name:       "a value that spells the name alone, in a body with no list at all",
			body:       `UPDATE probe SET note = 'batch as it was' WHERE id IN (SELECT id FROM batch)`,
			countValue: "SELECT count(*) FROM probe WHERE note = 'batch as it was'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
				"000002_fill.up.sql":  windowShapesData(tc.body),
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "prose", Files: files}); err != nil {
				t.Fatalf("the window refused a body that binds nothing of its own: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, tc.countValue); n != 12 {
				t.Errorf("%d of 12 rows carry the value: the window guard read a literal as a binding", n)
			}
			if rows := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); rows != 0 {
				t.Errorf("%d progress rows left by a drain that finished", rows)
			}
		})
	}
}
