package db_test

// review14_a_rekey_the_window_reader_does_not_walk_is_drained_to_its_bound_test.go is the
// fourteenth round's case for the two bodies that move the key the cursor runs over in a
// shape `movesTheWindowKey` does not walk.
//
// The claim under test is the one `kit/db/backfill.go` makes above that reader — the drain's
// one assumption is "refused rather than trusted": the cursor is a key, so every conclusion
// the drain draws from it (a short window is the end because nothing is left above that key,
// a resumed cursor has rows behind it and not in front of it, a committed row is never taken
// again) holds only of a key that stays where it was. The thirteenth review found the body
// that writes the key; the round refused it by name, reading "the assignment targets of the
// statements that name the drained table" (`migrations/README.md`).
//
// Two bodies still move the key past that reader:
//
//   - `UPDATE probe p SET id = …` — an alias written without `AS`, ordinary SQL and one word
//     away from the spelling the reader walks (`UPDATE probe AS p`, which it does walk). The
//     give-up sentence in `migrations/README.md` names an upsert's `DO UPDATE SET` and a
//     `MERGE` arm; it does not name this, and this statement does name the drained table.
//   - `WITH moved AS (DELETE FROM probe … RETURNING *) INSERT INTO probe SELECT id + 1000 …`
//     — one statement, which is what the window asks for, that takes the key away and hands
//     it back above the cursor. No UPDATE, so no assignment list to read.
//
// Both are the harm the refusal exists to prevent, measured rather than argued: ten rows and
// a window of five, so every window is a full one and the drain never meets the short window
// that would end it. It runs to the bound a migration gives itself and reports
// `ErrBackfillBudget`; through `db.Backfill` it re-writes rows on every tick forever, which
// is the release that never finishes.
//
// The assertions reach the fixed behaviour through what a refusal leaves, not through the
// sentence a defect prints: nothing resumable, nothing applied, no row whose key moved.

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADataBodyThatMovesItsKeyPastTheReaderIsRefusedRatherThanDrainedToItsBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "an UPDATE that names the drained table under an alias written without AS",
			body: `UPDATE probe p SET id = id + 1000, done = true WHERE p.id IN (SELECT id FROM batch)`,
		},
		{
			name: "a statement that takes the key away and hands it back above the cursor",
			body: `WITH moved AS (DELETE FROM probe WHERE id IN (SELECT id FROM batch) RETURNING id)
INSERT INTO probe (id) SELECT id + 1000 FROM moved`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				// Ten rows and a window of five: no window ever comes back short, so a
				// body that puts its own rows above the cursor has no end to reach.
				"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 10) g`)},
			}
			source := db.MigrationSource{Owner: "walked", Files: files}
			if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
				t.Fatal(err)
			}
			files["000002_rekey.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
` + tc.body)}

			err := db.Migrate(t.Context(), migrateURL, source)
			admin := dbtest.Open(t, migrateURL)

			if err == nil {
				t.Fatalf("the drain reported success over a table whose key its body moves: %d rows now carry a key above 1000",
					countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 1000"))
			}
			if errors.Is(err, db.ErrBackfillBudget) {
				t.Errorf("the run was stopped by the bound a migration gives itself rather than refused by the window: %v", err)
			}
			// What a refusal owes: nothing resumable, and nothing of the owner applied. Both
			// read out of the two ledgers, so the case does not depend on a fix's wording.
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill WHERE owner = 'walked'"); n != 0 {
				t.Errorf("%d progress rows left behind: a body the window cannot bound was begun, so the drain is resumable over a table it will never empty", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'walked' AND version = 2"); n != 0 {
				t.Errorf("%d history rows for the re-keying file, want none", n)
			}
			// And the harm itself: no row's key moves at all, because once the cursor is
			// chasing its own writes the only thing that stops the run is a bound.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 1000"); n != 0 {
				t.Errorf("%d rows carry a key moved above the window's start: each window committed rows an earlier window had already written", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe"); n != 10 {
				t.Errorf("%d rows in the table after a refused drain, want the 10 the owner created", n)
			}
		})
	}
}
