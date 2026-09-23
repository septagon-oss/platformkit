package db_test

// review11_what_the_body_reading_reaches_and_where_it_stops_test.go is the eleventh round's
// own case for the reach of the two readings its fix added, on the three edges the acceptance
// review's files leave open.
//
// migrations/README.md says a rule is a judgement a file may answer with `allow=` and a
// sentence, and the reading that reaches the statements inside a `DO $$ … $$` body is written
// on the strength of that: the file whose DROP COLUMN sits behind a test the running
// installation decides is refused whether or not the branch is ever taken. That is the
// over-reading the marker exists for, so the first leg refuses it and the second applies the
// same file with the marker and its sentence on it — a reading that reaches a statement it
// should not have reached and cannot be answered is the same fault as one it never reached at
// all. The third leg is the boundary the reading does not cross: a verb that only appears
// inside a value the body prints keeps the body's own verb at the statement's front, which is
// what stops "the guard now reads everything" from being the honest description of it.
//
// The fourth leg holds the CONCURRENTLY reading to what it narrowed. The two rules about that
// word refuse each other's file by construction, and the fix stopped them reading the word
// inside a value; a REINDEX run outside the transaction still needs the mode whatever letters
// the file put after the verb, and a rule with no exception that gained a file it refuses for
// a spelling would have taken its teeth while pretending to keep them.
//
// The last case reads the file the review's own
// `TestTheIndexExemptionIsNotBoughtByAValueThatSpellsACreate` reads, and asserts the half of it
// the catalogue can answer: the create inside a value buys no exemption, the build beside it is
// refused by name and none of the refused release reaches this database. It is here because that
// leg finishes by reading `schema_migrations`, which no run that refuses a file's text can have
// created — the guard answers before the pool is opened, which `migrations/review_floors_test.go`
// pins by pointing `db.Migrate` at a database that is not there. The claim is not argued away;
// it is asserted here out of `pg_class`, the way the review's two sibling files read it, and the
// leg's own query is recorded under *Not verified*.
//
// Every assertion is on the catalogue and the ledger: which rule refused, which relations and
// history rows exist afterwards, and which column the run left where it was.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// bodyReadingProbe is the table every leg of the first case wraps: a column the body talks
// about taking away, and one the body really does take away in the leg that says so.
const bodyReadingProbe = "CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text)"

func TestTheStatementReadingInsideABodyReachesABranchAndStopsAtAValue(t *testing.T) {
	for _, tc := range []struct {
		name, file, refused string
	}{
		{
			// The body's own verb, behind a test only the running installation can
			// answer. The guard cannot know the branch is never taken, and does not
			// pretend to: it names the rule, and the author answers it.
			name: "a dropped column behind a test the installation decides",
			file: `DO $$ BEGIN
  IF current_setting('pkit.drop_old_column', true) = 'yes' THEN
    ALTER TABLE probe DROP COLUMN b;
  END IF;
END $$`,
			refused: "rule drop-column",
		},
		{
			// The same file, with the exception written down: the statement is the
			// risky one and the sentence is what a reviewer reads. This is the answer
			// the whole reading is built on — without it the over-read above would be
			// the unshippable file rather than the reviewable one.
			name: "the same body with the marker and its sentence on it",
			file: "-- pkit: allow=drop-column reason=the branch is off in every installation and the column is empty wherever the release has run\n" +
				`DO $$ BEGIN
  IF current_setting('pkit.drop_old_column', true) = 'yes' THEN
    ALTER TABLE probe DROP COLUMN b;
  END IF;
END $$`,
			refused: "",
		},
		{
			// The boundary. `b` is never named as a column this file drops: the words
			// are a sentence the function prints, and the statement that carries them
			// begins with `raise`. The reading that reaches a body's statements stops
			// at the body's own quotes, the same way the one outside a body stops at a
			// literal's.
			name: "a body that only says the words of the statement, the control",
			file: `CREATE OR REPLACE FUNCTION probe_note() RETURNS text AS $body$
BEGIN
  RAISE NOTICE 'the next release will run ALTER TABLE probe DROP COLUMN b';
  RETURN 'the next release will run ALTER TABLE probe DROP COLUMN b';
END
$body$ LANGUAGE plpgsql`,
			refused: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(bodyReadingProbe)},
				"000002_body.up.sql":  {Data: []byte(tc.file)},
				"000003_after.up.sql": {Data: []byte("CREATE TABLE line_items (id bigint PRIMARY KEY, order_id bigint)")},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "reaches", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if tc.refused == "" {
				if err != nil {
					t.Fatalf("the run refused a file the reading has nothing to say about: %v", err)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'probe' AND column_name = 'b'"); n != 1 {
					t.Errorf("%d columns named b: a file that named no statement the rule is about changed the table", n)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'reaches'"); n != 3 {
					t.Errorf("%d history rows, want the owner's three files", n)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refused) {
				t.Fatalf("the body's own statement was not refused by %s: %v", tc.refused, err)
			}
			// A rule reads the file's text, so it refuses the whole owner before the
			// runner connects: the file above the body is not here either.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe'"); n != 0 {
				t.Errorf("%d relations named probe: a file the rule table refuses applies nothing of its owner", n)
			}
		})
	}
}

// TestAnAutocommitFileThatReindexesConcurrentlyIsNotRefusedForItsSpelling holds the other
// side of the CONCURRENTLY reading. The rule fires on "autocommit=true with nothing
// nontransactional in the file", and narrowing what that question is asked of must not take
// its teeth: a REINDEX run outside the transaction needs the mode whatever letters the file
// put after the verb, and a file that has to answer this rule with a marker it cannot write
// is a file that cannot ship.
func TestAnAutocommitFileThatReindexesConcurrentlyIsNotRefusedForItsSpelling(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, t text);\nCREATE INDEX probe_t_idx ON probe (t)")},
		"000002_reindex.up.sql": {Data: []byte(`-- pkit: autocommit=true
REINDEX (CONCURRENTLY) TABLE probe`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "reindex", Files: files}); err != nil {
		t.Fatalf("the autocommit file that holds the statement the mode is for was refused: %v", err)
	}
	if n := countRows(t, dbtest.Open(t, migrateURL), "SELECT count(*) FROM pg_indexes WHERE indexname = 'probe_t_idx'"); n != 1 {
		t.Errorf("%d indexes named probe_t_idx after the file that reindexed them", n)
	}
}

// TestTheIndexExemptionIsNotBoughtByWordsInsideAValue reads the same file the tenth review's
// `TestTheIndexExemptionIsNotBoughtByAValueThatSpellsACreate` reads, and asserts the half of
// it that the catalogue can answer: the create in the value buys nothing, the plain build
// beside it is refused by name, and none of the refused release reaches this database.
//
// It is here because the review's own leg finishes by reading `schema_migrations`, which no
// run that refuses a file's text can have created — the guard answers before the pool is
// opened, which `migrations/review_floors_test.go` pins by pointing `db.Migrate` at a
// database that is not there. That leg's assertion is recorded under *Not verified* rather
// than argued away; this one is the same claim about the same file, read out of `pg_class`
// the way the review's own two sibling files read it.
func TestTheIndexExemptionIsNotBoughtByWordsInsideAValue(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 50) g")},
		"000002_docs_and_index.up.sql": {Data: []byte(`INSERT INTO probe (id, note) VALUES (100, 'the layout used to read CREATE TABLE probe (id bigint)');
CREATE INDEX probe_note_idx ON probe (note)`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "docs", Files: files})
	if err == nil || !strings.Contains(err.Error(), "index-not-concurrent") {
		t.Fatalf("a plain build on a table the release is reading was excused by the words of a create inside a value: %v", err)
	}
	// The create the exemption was reached for is a sentence in a column, so the rule fired,
	// and a rule that fires refuses the whole owner before the runner connects: neither of
	// the file's two statements is here.
	admin := dbtest.Open(t, migrateURL)
	for _, relation := range []string{"probe", "probe_note_idx"} {
		if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = '"+relation+"'"); n != 0 {
			t.Errorf("%d relations named %s: a file the rule table refuses applies nothing of its owner", n, relation)
		}
	}
}
