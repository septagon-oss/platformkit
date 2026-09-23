package db_test

// review11_a_statement_inside_a_dollar_body_is_no_statement_at_all_test.go is the tenth
// round's acceptance-review case for what the rule table reads a body as.
//
// migrations/README.md: "The engine reads text with comments stripped, not a parse tree — the
// runner is not a SQL parser — so a statement inside a dollar-quoted body can be flagged, and
// the answer is the marker." The tenth round's commit states the same division the other way
// round: "every rule that carries an exception keeps the inside reading, whose false positive is
// what the marker exists for".
//
// Neither is what three of the four rules that carry a marker do. `rewritesAColumnType`,
// `indexesANewTable` and `dropsAColumn` ask their question of a statement's *front* —
// `^alter table`, `^create index` — and the inside split does not give them the body's
// statements: it cuts `DO $$ BEGIN ALTER TABLE probe DROP COLUMN b; END $$` after the
// semicolon *inside* the body, so the piece still begins `do $$ begin alter table` and no rule
// is anchored there. The reading surfaces the body's semicolons, never its verbs. So the file
// that wraps the rewrite, the plain build or the dropped column in a `DO` block — the ordinary
// way to write DDL that has to survive having already run — is applied with no rule named, no
// remedy offered and no marker to write: a rule that did not fire cannot be excepted, so
// `unused-allow` refuses the author who tried to declare the risk.
//
// The harm is the three rules' own: a full rewrite under `ACCESS EXCLUSIVE`, a `SHARE` lock over
// a table with writers, and a column taken away from the release that is running. The brief's
// deliverable 3 states each of them is "refused with the rule's name".
//
// Every leg asserts the fixed behaviour — the rule the repository's own table names, and what
// the catalogue holds afterwards — and each has a control written as a plain statement, which
// is refused today and has to stay refused.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAStatementWrappedInADOBlockIsStillTheStatementTheRuleRefuses(t *testing.T) {
	const seed = "CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text);\nINSERT INTO probe (id, a, b) SELECT g, 'x', 'y' FROM generate_series(1, 20) g"
	for _, tc := range []struct {
		name, file, refused, built string
	}{
		{
			name:    "a dropped column inside a DO block",
			file:    "DO $$ BEGIN ALTER TABLE probe DROP COLUMN b; END $$",
			refused: "rule drop-column",
		},
		{
			name: "the conditional drop every idempotent file writes",
			file: `DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'probe' AND column_name = 'b') THEN
    ALTER TABLE probe DROP COLUMN b;
  END IF;
END $$`,
			refused: "rule drop-column",
		},
		{
			name:    "a type change inside a DO block",
			file:    "DO $$ BEGIN ALTER TABLE probe ALTER COLUMN a TYPE varchar(8); END $$",
			refused: "rule alter-column-type",
		},
		{
			name:    "a plain index build inside a DO block",
			file:    "DO $$ BEGIN CREATE INDEX probe_a_idx ON probe (a); END $$",
			refused: "rule index-not-concurrent",
			built:   "probe_a_idx",
		},
		{
			name:    "the same dropped column as a plain statement, the control",
			file:    "ALTER TABLE probe DROP COLUMN b",
			refused: "rule drop-column",
		},
		{
			name:    "the same type change as a plain statement, the control",
			file:    "ALTER TABLE probe ALTER COLUMN a TYPE varchar(8)",
			refused: "rule alter-column-type",
		},
		{
			name:    "the same plain build as a plain statement, the control",
			file:    "CREATE INDEX probe_a_idx ON probe (a)",
			refused: "rule index-not-concurrent",
			built:   "probe_a_idx",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql":  {Data: []byte(seed)},
				"000002_change.up.sql": {Data: []byte(tc.file)},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "wrapped", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				// What the database holds afterwards is the reason this is not a documentation
				// quibble: the name is gone, the rewrite ran, or the lock was taken over a table
				// with writers — and no rule was named anywhere in the run.
				note := ""
				if n := countRows(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'probe' AND column_name = 'b'"); n == 0 {
					note += "; column b is gone from the table"
				}
				if n := countRows(t, admin, "SELECT count(*) FROM pg_indexes WHERE indexname = 'probe_a_idx'"); n == 1 {
					note += "; the plain build probe_a_idx was taken over a table with writers"
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'wrapped'"); n != 2 {
					t.Errorf("%d history rows, want the whole owner applied: the refusal the leg is about has not happened", n)
				}
				t.Fatalf("%s: applied with no rule named%s", tc.name, note)
			}
			if !strings.Contains(err.Error(), tc.refused) {
				t.Fatalf("the run refused for something other than %s: %v", tc.refused, err)
			}
			// A rule reads the file's text, so it refuses the owner before the runner connects.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe'"); n != 0 {
				t.Errorf("%d relations named probe: a file the rule table refuses applies nothing of its owner", n)
			}
		})
	}
}

// TestTheMarkerForAStatementInsideABodyIsTheOneTheAuthorCanReach is the other half: the
// repository tells a reader who disagrees with a rule to answer it with `allow=` and a sentence.
// For a statement the guard cannot see there is no such answer, because a marker for a rule that
// did not fire is the `unused-allow` refusal. A rule that cannot fire cannot be excepted, which
// leaves the file with the risk declared and the run refusing it for the declaration.
func TestTheMarkerForAStatementInsideABodyIsTheOneTheAuthorCanReach(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text)")},
		"000002_change.up.sql": {Data: []byte(`-- pkit: allow=drop-column reason=the column is empty in every installation and the body checks it
DO $$ BEGIN ALTER TABLE probe DROP COLUMN b; END $$`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "declared", Files: files})
	if err != nil {
		t.Fatalf("a file that declares the rule it is about, with its reason, was refused: %v", err)
	}
}
