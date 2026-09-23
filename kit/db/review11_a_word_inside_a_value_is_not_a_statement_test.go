package db_test

// review11_a_word_inside_a_value_is_not_a_statement_test.go is the tenth round's
// acceptance-review case for the two rules about `CONCURRENTLY`.
//
// The tenth round's own file, `review8_an_autocommit_function_body_test.go`, records why
// `autocommit-not-rerunnable` moved to the cut PostgreSQL makes, and closes with:
//
//	`index-concurrent-without-autocommit` and `autocommit-without-concurrently` ask their
//	question of the whole body and so never consult a split; the rules that read a dollar
//	body from the inside on purpose all carry a marker.
//
// The first half is true — both call `migrationText.any`, a regexp over the body — and it is
// offered as if it settled the question the round adopted as a principle: a refusal no
// `allow=` reaches may not rest on a reading that is wrong about what the file does, because
// the file it refuses is then not correctable. Neither rule asks where a statement begins or
// ends; both ask whether the *word* `concurrently` appears anywhere in the file's text, and
// the text they read (`migration.plain`) keeps the contents of every literal and
// dollar-quoted value. So a `phase=data` body whose value carries the word is refused by a
// rule whose remedy the header grammar then refuses to give it, and an autocommit file with
// no nontransactional statement in it is excused by a word inside a value.
//
// Whether a rule carries a marker is a different question from whether it reads a value's
// contents, and both of these rules state none: migrations/README.md's table says `none` for
// both, SPECIFY's says "none: it is a fact about Postgres, not a judgement", and the same
// SPECIFY table over the same three rules calls them "immutable as stated, **correctable as
// authored**: they refuse the shape". The first three legs are the file that is not correctable
// as authored; the next two are the rule whose teeth a value takes; the last is the same
// misreading on the `CREATE TABLE` capture, which decides an exemption.
//
// Every assertion is on the run's outcome and the catalogue, never on a message a defect
// prints: which files applied, which value the rows carry, which index exists, and the name of
// the rule the repository's own table says refuses the file.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const review11Seed = "CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g"

// review11Backfill is the same one-windowed-update body spelled three ways: with the words of
// an index build inside a dollar-quoted value, with them in an ordinary literal, and without
// them. The third is the control — it shows that what the first two answer has to do with the
// word and not with the shape of the file.
var review11Backfill = map[string]string{
	"a data body whose value carries the words of an index build": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = $t$rebuild with CREATE INDEX CONCURRENTLY$t$ WHERE id IN (SELECT id FROM batch)`,
	"a data body whose plain literal carries the words of an index build": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = 'rebuild with CREATE INDEX CONCURRENTLY' WHERE id IN (SELECT id FROM batch)`,
	"the same data body with no such words in its value, the control": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = 'rebuild this index offline' WHERE id IN (SELECT id FROM batch)`,
}

func TestADataValueThatSpellsConcurrentlyIsStillOneWindowedStatement(t *testing.T) {
	for _, spelling := range []string{
		"a data body whose value carries the words of an index build",
		"a data body whose plain literal carries the words of an index build",
		"the same data body with no such words in its value, the control",
	} {
		t.Run(spelling, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(review11Seed)},
				"000002_note.up.sql":  {Data: []byte(review11Backfill[spelling])},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "word", Files: files})
			if err != nil {
				t.Fatalf("the run refused a data file for a word inside a value it writes: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note LIKE 'rebuild%'"); n != 25 {
				t.Errorf("%d of 25 rows carry the value: the drain did not run this file to its end over its windows", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'word' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for the data file, want 1", n)
			}
			// Nothing the value spells is built, and nothing is left resumable.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_indexes WHERE indexname = 'probe_note_idx'"); n != 0 {
				t.Errorf("%d indexes named probe_note_idx: the guard ran the file's value", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d drain progress rows left behind by a drain that finished", n)
			}
		})
	}
}

// TestAnAutocommitFileWhoseOnlyConcurrentlyIsDataIsStillRefused is the same misreading on the
// side where it costs the guard rather than the author. migrations/README.md's table says
// `autocommit-without-concurrently` fires on "`autocommit=true` with nothing nontransactional
// in the file"; SPECIFY's says "in a file with no `CONCURRENTLY` statement". The first file
// below has no nontransactional statement — the words are inside a value the function body
// writes — and the control beside it, word for word the same file without that value, is
// refused. A value, which is data, answers the one question this rule exists to make
// impossible: taking a file out of the transaction that gives every other migration
// all-or-nothing, for no reason.
func TestAnAutocommitFileWhoseOnlyConcurrentlyIsDataIsStillRefused(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{
			name: "the words are inside a value the body writes",
			file: `-- pkit: autocommit=true
CREATE OR REPLACE FUNCTION probe_reindex() RETURNS void AS $body$
BEGIN RAISE NOTICE '%', 'rebuild with CREATE INDEX CONCURRENTLY'; END
$body$ LANGUAGE plpgsql`,
		},
		{
			name: "the same body with no such value, the control",
			file: `-- pkit: autocommit=true
CREATE OR REPLACE FUNCTION probe_reindex() RETURNS void AS $body$
BEGIN RAISE NOTICE '%', 'nothing'; END
$body$ LANGUAGE plpgsql`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql":   {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, t text)")},
				"000002_reindex.up.sql": {Data: []byte(tc.file)},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "word", Files: files})
			if err == nil || !strings.Contains(err.Error(), "autocommit-without-concurrently") {
				t.Fatalf("an autocommit file with no statement that needs the mode was not refused by autocommit-without-concurrently: %v", err)
			}
			// The rule reads the file's text, so it refuses before the runner connects and
			// nothing of the owner is in this database at all. The control proves the refusal is
			// about the mode and not about the file's shape: the same two files, with the value
			// absent, answer the same way.
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe'"); n != 0 {
				t.Errorf("%d relations named probe: a file the rule table refuses applies nothing of its owner", n)
			}
		})
	}
}

// TestTheIndexExemptionIsNotBoughtByAValueThatSpellsACreate is the third shape of the same
// misreading, on the rule that does carry a marker. `newMigrationText` captures `reCreateTable`
// over the whole body, contents of literals included, so the words of a `CREATE TABLE` inside a
// value a file is writing mark that table as one this file created outright — "nothing is
// reading it yet" — and the plain build beside it is excused by a create that is data. What the
// exemption is for is the `SHARE` lock over a table the release is reading, which is the one
// thing the sentence in the value says nothing about.
func TestTheIndexExemptionIsNotBoughtByAValueThatSpellsACreate(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 50) g")},
		// One file, because the exemption is a fact about one file: "this file creates the
		// table outright, so nothing is reading it yet". The create below is a value the file
		// writes, and the build below it is over the table file 1 filled and the release reads.
		"000002_docs_and_index.up.sql": {Data: []byte(`INSERT INTO probe (id, note) VALUES (100, 'the layout used to read CREATE TABLE probe (id bigint)');
CREATE INDEX probe_note_idx ON probe (note)`)},
	}
	// The ledger this case counts has to be created by a run nothing refuses. The guard
	// answers from the files inside `readMigrations`, before the pool opens, so the run
	// this case asks for leaves no `schema_migrations` to query at all — the leg that read
	// it until the twelfth review ruled it superseded (decision 0008's amendment of
	// 2026-09-23, case 3) could only be satisfied by the defect it reports, which is a
	// proof that cannot pass. Its sibling legs at :115 and :140 ask `pg_class` for the
	// same reason, and `kit/db/review_guarantees_test.go` requires that a refused
	// composition leave the runner's own two tables uncreated.
	//
	// So the claim is made where it can be made: one owner applies, which is what puts the
	// ledger in this schema, and the refused owner is then counted against a table that
	// exists whatever this file's rule answers — plus the catalogue, which answers either
	// way. A refused owner writes no history row beside an owner that did apply, and its
	// `CREATE TABLE` never reaches the server.
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "before", Files: fstest.MapFS{
		"000001_rows.up.sql": {Data: []byte("CREATE TABLE before_rows (id bigint PRIMARY KEY)")},
	}}); err != nil {
		t.Fatalf("the owner this case migrates to have a ledger did not apply: %v", err)
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "docs", Files: files})
	if err == nil || !strings.Contains(err.Error(), "index-not-concurrent") {
		t.Fatalf("a plain build on a table the release is reading was excused by the words of a create inside a value: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'before'"); n != 1 {
		t.Fatalf("%d history rows for the owner that applied; the ledger this case counts is not there", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'docs'"); n != 0 {
		t.Errorf("%d rows of the refused release applied; a rule that reads the file's text refuses the owner before the runner connects", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe' AND relnamespace = current_schema()::regnamespace"); n != 0 {
		t.Errorf("%d relations named probe: the refused release reached the server, so the words of a create inside a value bought the exemption a create the file runs would buy", n)
	}
}
