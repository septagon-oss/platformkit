package db_test

// review8_ddl_inside_a_value_test.go is the tenth round's case for how far the ninth
// review's first finding reaches.
//
// The finding was the executor's: a `phase=data` file whose body is one statement was
// refused as two because the `;` sat inside a `$tag$ … $tag$` value it was writing
// (`len(splitStatements(m.plain)) > 1`). The same split fed one rule, and that rule's
// answer is worse than the executor's: `data-with-ddl` fires on a statement that *begins*
// with ALTER, CREATE or DROP, and it states no exception, so a value carrying
// `…; create table ghost …` was refused for changing the schema and the `allow=` that
// would have answered it was refused as a bypass. The sentence then describes a DDL
// statement the file does not contain, and its remedy — "split the file: the DDL is a
// schema file" — names a file the author has already written.
//
// So the split is split: the count and the front anchor are asked of the cut PostgreSQL
// makes, and everything else the rule table reads keeps the reading that looks inside a
// dollar body, because that over-reading is what a marker is for. This file holds the new
// reading to what it changes and nothing more:
//
//   - a value that carries DDL after a semicolon is data (the file drains, `ghost` is
//     never created) — the leg that fails before the change;
//   - DDL written as a statement after a semicolon outside any value still fires the rule,
//     and still with no exception to except it, which is the leg that would catch a fix
//     that took the rule's teeth instead of its misreading;
//   - a body that is genuinely two statements *and* carries a value is still refused by the
//     executor, so the count still counts: `review3_data_file_shape_test.go` shows the
//     valueless spelling of that file, and this is the one where the semicolon inside the
//     value is what a wrong split would have counted.
//
// Every assertion is on the catalogue or the ledger, not on a message: which rule refused,
// and whether anything was written.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// ddlValueSeed is the table the window drains, with the text column the value goes into.
const ddlValueSeed = `CREATE TABLE probe (id bigint PRIMARY KEY, note text, done boolean NOT NULL DEFAULT false);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g`

// ddlValueData is one phase=data body: the window's own WHERE, and a value that carries
// punctuation, DDL words and — in the two-statement spelling — a real second statement.
var ddlValueData = map[string]string{
	"a value carrying a semicolon and the words of a DDL statement": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = $t$shipped; create table ghost (id int)$t$ WHERE id IN (SELECT id FROM batch)`,
	"a DDL statement of its own, after a semicolon no value is holding": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = 'shipped' WHERE id IN (SELECT id FROM batch);
CREATE TABLE ghost (id int)`,
	"the same DDL statement, marked": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
-- pkit: allow=data-with-ddl reason=the DDL is one column of the row every tenant needs at once
UPDATE probe SET note = 'shipped' WHERE id IN (SELECT id FROM batch);
CREATE TABLE ghost (id int)`,
	"a value carrying a semicolon beside a second statement of its own": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = $t$shipped; still one value$t$ WHERE id IN (SELECT id FROM batch);
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`,
}

func TestADataFilesDDLIsWhatTheServerRunsAndNotWhatAValueCarries(t *testing.T) {
	for _, spelling := range []string{
		"a value carrying a semicolon and the words of a DDL statement",
		"a DDL statement of its own, after a semicolon no value is holding",
		"the same DDL statement, marked",
		"a value carrying a semicolon beside a second statement of its own",
	} {
		t.Run(spelling, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(ddlValueSeed)},
				"000002_note.up.sql":  {Data: []byte(ddlValueData[spelling])},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "value", Files: files})
			admin := dbtest.Open(t, migrateURL)
			switch spelling {
			case "a value carrying a semicolon and the words of a DDL statement":
				if err != nil {
					t.Fatalf("the run refused a data file that writes the words of a DDL statement as one of its values: %v", err)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'shipped; create table ghost (id int)'"); n != 25 {
					t.Errorf("%d of 25 rows carry the value: the drain did not run this file to its end over its three windows", n)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'value' AND version = 2"); n != 1 {
					t.Errorf("%d history rows for the data file, want 1", n)
				}

			case "a DDL statement of its own, after a semicolon no value is holding":
				if err == nil || !strings.Contains(err.Error(), "data-with-ddl") {
					t.Fatalf("a data file that really does change the schema was not refused by data-with-ddl: %v", err)
				}
				// The rule reads the file's text, so it refuses before the runner connects and
				// the owner's earlier file goes with the refused one: nothing of this source
				// reached the database. That is the property a marker could not buy, and the
				// ledger does not exist to be read afterwards.
				if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe'"+
					" AND relnamespace = current_schema()::regnamespace"); n != 0 {
					t.Errorf("%d relations named probe: a refused file let the file before it apply", n)
				}

			case "the same DDL statement, marked":
				if err == nil || !strings.Contains(err.Error(), "data-with-ddl") || !strings.Contains(err.Error(), "excepts nothing") {
					t.Fatalf("an allow= on the rule with no exception did not reach the refusal it is refused as: %v", err)
				}

			case "a value carrying a semicolon beside a second statement of its own":
				if err == nil || !strings.Contains(err.Error(), "a data file is one statement") {
					t.Fatalf("a body of two statements, one of them carrying a value with a semicolon in it, was not refused as two: %v", err)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
					t.Errorf("%d drain progress row(s) left behind by a refusal of a shape that can never drain", n)
				}
			}
			// The one fact all four share: words inside a value, or a statement the run
			// refused, have not put a table in this database.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'ghost'"+
				" AND relnamespace = current_schema()::regnamespace"); n != 0 {
				t.Errorf("%d relations named ghost: a phase=data file does not get to create one, whatever its values say", n)
			}
		})
	}
}
