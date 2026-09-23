package db_test

// review12_a_data_file_is_drained_over_a_key_the_server_can_order_but_not_maximum_test.go is
// the twelfth round's case for the claim at kit/db/README.md:95:
//
//	"The key may be of any single-column primary key type: the cursor travels as the key
//	cast to text and comes back as a comparison against that type's own name, read from
//	the catalogue, so the drain asks PostgreSQL for the ordering instead of keeping a
//	list of the types it is willing to name."
//
// It asks the ordering for the window (`ORDER BY w LIMIT n`) and the *maximum* for the top
// of it: `SELECT count(*), coalesce(max(w)::text, '') FROM (…) windowed`. PostgreSQL has no
// `max` aggregate for `uuid` or for `bytea`, so the drain answers those two tables with a
// server error at the first window. That is not a type the kernel declined to name; it is
// the type every entity table in this repository is keyed by — `tenants`, `users`,
// `platformkit_outbox`, and module/user, auth, audit, billing, content, file, notification
// and site each create an `id uuid PRIMARY KEY` — and `modules/auth` keys its token hashes
// `bytea PRIMARY KEY`. So no `phase=data` file can drain any table this kernel ships, and
// the brief's own first consumer ("`search`'s second migration … changes a table") is one
// of them. Measured end to end through the release step the delivery wrote: with the
// ten-thousand-row fixture on a copy of the previous release,
//
//	rehearse: candidate: 680fd1e with a dirty tree
//	platformkit: db: migrate: platformkit/000027_rehearse_backfill.up.sql: measuring the
//	    next batch of users: ERROR: function max(uuid) does not exist (SQLSTATE 42883)
//	rehearse: failed: 1 finding(s); exit 1
//
// The failure is not even a refusal with an id: the four below-threshold refusals name
// themselves, and this one arrives as PostgreSQL's own text. And it leaves the progress row
// behind, so the drain is "in flight" forever: `planOwner` resumes a drain it finds started,
// every later `Migrate` and every worker tick repeats the same failing query, and the owner
// never converges.
//
// What the case asserts is the README's claim, and it can pass: `uuid` and `bytea` are both
// orderable, so asking the server for the top of the window the same way the window itself
// is taken (`ORDER BY w DESC LIMIT 1`) returns the key for every orderable type, and its
// `::text` rendering round-trips through `$1::uuid` and `$1::bytea` — measured on the pinned
// server, where the top of a bytea window renders `\x33` and `'\x33'::bytea = '3'::bytea`.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADataFileIsDrainedOverAKeyTheServerCanOrderButNotMaximum(t *testing.T) {
	for _, tc := range []struct{ name, seed string }{
		{"uuid, the key every entity table of this repository has",
			`CREATE TABLE probe (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), note text NOT NULL DEFAULT '');
			 INSERT INTO probe (id) SELECT gen_random_uuid() FROM generate_series(1, 12) g`},
		{"bytea, the key modules/auth gives its token hashes",
			`CREATE TABLE probe (id bytea PRIMARY KEY, note text NOT NULL DEFAULT '');
			 INSERT INTO probe (id) SELECT (g::text)::bytea FROM generate_series(1, 12) g`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(tc.seed)},
				"000002_note.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
UPDATE probe SET note = 'done' WHERE note = '' AND id IN (SELECT id FROM batch)`)},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "anykey", Files: files}); err != nil {
				t.Fatalf("the drain could not window a table the README says it windows: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 12 {
				t.Errorf("%d of 12 rows drained over a window the kernel wrote: the drain stopped before the table's end", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'anykey' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for a drain that was asked to finish", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows left by a drain that finished: each later run will take this one up again", n)
			}
		})
	}
}
