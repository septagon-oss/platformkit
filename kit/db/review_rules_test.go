package db_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// These cases are the review's. Each one asserts what migrations/README.md and
// kit/db's own rule table say about a file, and each is a file the runner accepts
// today. They are left failing rather than deleted: the assertion is the correct
// behaviour, and the passing branch is the rule table or the executor catching up.

// reviewProbeSource is the ordinary first file — a table with the single-column
// primary key a drain needs, and rows — beside the file under test.
func reviewProbeSource(second string) db.MigrationSource {
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text, passes integer NOT NULL DEFAULT 0);
CREATE INDEX probe_a ON probe (a);
INSERT INTO probe (id) SELECT g FROM generate_series(1,25) g`)},
	}
	if second != "" {
		files["000002_rule.up.sql"] = &fstest.MapFile{Data: []byte(second)}
	}
	return db.MigrationSource{Owner: "probe", Files: files}
}

// TestARuleWithNoExceptionRefusesItsMarker. migrations/README.md's exception
// column says `none` for four rules — three because they state a fact about
// PostgreSQL or about what the autocommit mode costs, one because the remedy is
// to split the file. checkRules excepts any rule whose name an allow= carries,
// so the marker the table says does not exist works, and the file reaches the
// server. Two of the four then fail at deploy time with PostgreSQL's own message
// rather than the rule's, which is the second vocabulary the rule table exists to
// prevent; the other two apply a file the kernel says it will not run.
func TestARuleWithNoExceptionRefusesItsMarker(t *testing.T) {
	for _, tc := range []struct{ name, rule, file string }{
		{
			name: "CONCURRENTLY inside the runner's transaction",
			rule: "index-concurrent-without-autocommit",
			file: "CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)",
		},
		{
			name: "an autocommit file with nothing nontransactional in it",
			rule: "autocommit-without-concurrently",
			file: "-- pkit: autocommit=true\nCREATE TABLE other (x text)",
		},
		{
			name: "an autocommit statement that refuses a second run",
			rule: "autocommit-not-rerunnable",
			file: "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY probe_b ON probe (b)",
		},
		{
			// The data file is one statement and its body is DDL, so the only rule
			// data-with-ddl is about. data-body-unbounded is marked beside it because
			// the marker for it exists, and its own exception is documented.
			name: "DDL in a data file",
			rule: "data-with-ddl",
			file: "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\n" +
				"-- pkit: allow=data-body-unbounded reason=the body bounds itself\n" +
				"ALTER TABLE probe ADD COLUMN d text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			// The marker as an author would write it, with the sentence.
			marked := "-- pkit: allow=" + tc.rule + " reason=a sentence a reviewer would read\n" + tc.file
			if tc.rule == "data-with-ddl" {
				marked = "-- pkit: allow=data-with-ddl reason=a sentence a reviewer would read\n" + tc.file
			}
			err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(marked))
			if err == nil {
				t.Fatalf("the runner applied a file carrying allow=%s; the rule table documents no exception for it, so the marker is a bypass its own name hides:\n%s",
					tc.rule, marked)
			}
			if !strings.Contains(err.Error(), tc.rule) {
				t.Errorf("refusal %q does not name the rule %q whose exception does not exist", err, tc.rule)
			}
			if strings.Contains(err.Error(), "SQLSTATE") {
				t.Errorf("the file reached PostgreSQL and answered in its own vocabulary (%q); the rule refused before the connection", err)
			}
		})
	}
}

// TestTheTypeChangeRuleCatchesTheShortSpelling. The rule is about a full rewrite
// under ACCESS EXCLUSIVE, and ALTER TABLE's grammar makes COLUMN optional:
// `ALTER TABLE probe ALTER b TYPE varchar(64)` rewrites the table exactly as the
// spelling the rule matches does, and the guard reads a keyword rather than the
// operation.
func TestTheTypeChangeRuleCatchesTheShortSpelling(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, reviewProbeSource("ALTER TABLE probe ALTER b TYPE varchar(64)"))
	if err == nil {
		t.Fatal("a table rewrite without the COLUMN keyword was accepted; the rule refuses the rewrite, not the spelling")
	}
	if !strings.Contains(err.Error(), "alter-column-type") {
		t.Errorf("refusal %q does not name alter-column-type", err)
	}
}

// TestAnAutocommitFileMustSurviveItsOwnSuccess covers the other half of the
// statement the autocommit mode exists to run: the README says the rule fires on
// "an autocommit statement without IF NOT EXISTS / IF EXISTS", and a
// DROP INDEX CONCURRENTLY carries no IF EXISTS here. Its bytes run, its index
// goes, and the version it belongs to can never run again.
func TestAnAutocommitFileMustSurviveItsOwnSuccess(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(
		"-- pkit: autocommit=true\nDROP INDEX CONCURRENTLY probe_a"))
	if err == nil {
		// The statement is not re-runnable, which is what the rule is for: the
		// autocommit mode may commit it and still leave the version unapplied.
		admin := dbtest.Open(t, migrateURL)
		if _, again := admin.ExecContext(t.Context(), "DROP INDEX CONCURRENTLY probe_a"); again == nil {
			t.Fatal("the reviewed statement ran twice without error, so the rule's premise here is wrong and this case is the proof of that")
		}
		t.Fatal("an autocommit file whose one statement cannot be run again was accepted; DROP INDEX CONCURRENTLY needs IF EXISTS")
	}
	if !strings.Contains(err.Error(), "autocommit-not-rerunnable") {
		t.Errorf("refusal %q does not name autocommit-not-rerunnable", err)
	}
}

// TestADataFileThatBoundsItselfRunsOnce. A data file whose body bounds itself says
// so with allow=data-body-unbounded, and the runner's own comment says what that
// means: "It runs once, in this transaction, and the drain is over: there is no
// window to resume, and nothing to pretend otherwise." The decision whether a body
// goes through a window is made by matching `batch` in the file's raw text, while
// the rule that judged the same file matched it in the text with comments stripped
// and lower-cased. A body that mentions the window in a comment, or names it in
// another case, is therefore judged by one reader and executed by the other: the
// self-bounded statement runs once per window over the whole table, and the
// windowed statement runs once with no window at all.
func TestADataFileThatBoundsItselfRunsOnce(t *testing.T) {
	for _, tc := range []struct{ name, file, want string }{
		{
			name: "the exception, with the word batch in a comment",
			file: `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
-- pkit: allow=data-body-unbounded reason=one statement gives every row the same value
-- the batch is the whole table here, deliberately
UPDATE probe SET passes = passes + 1`,
			want: "the excepted body runs once, so every row is written by exactly one transaction",
		},
		{
			name: "the window named in another case",
			file: `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM BATCH)`,
			want: "SQL folds the case of an identifier, so the window relation is the window relation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			if err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(tc.file)); err != nil {
				t.Fatalf("migrate: %v (%s)", err, tc.want)
			}
			admin := dbtest.Open(t, migrateURL)
			// One pass over the table: every row carries the one increment the body
			// writes. A body run per window counts its own windows; a body run with
			// no window at all counts one but visited every row outside the batch.
			if extra := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1"); extra != 0 {
				t.Errorf("%d of 25 rows were written more than once; %s", extra, tc.want)
			}
		})
	}
}

// TestAnExceptionsReasonSaysSomething. The grammar table this task's specification
// states gives `reason` the domain "free text, ≥ 3 characters": the sentence is the
// whole content of an exception, and the parser already refuses a reason that is
// empty or all spaces. One character passes, which is the marker's claim of a
// review with nothing under it.
func TestAnExceptionsReasonSaysSomething(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(
		"-- pkit: allow=drop-column reason=x\nALTER TABLE probe DROP COLUMN b"))
	if err == nil {
		t.Fatal("allow=drop-column reason=x applied: a one-character reason is the empty reason with a letter in front of it")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("refusal %q does not name the reason= the operator has to write", err)
	}
}

// TestABatchOfZeroIsRefusedForWhatItActuallyIs. The header states its own domain for
// `batch` as 1…100000, and the grammar refuses a file whose value is out of it by
// naming the value and the domain. `batch=0` is the one value in that domain's
// neighbourhood that is not refused for what it is: the parsed header keeps a batch
// of zero indistinguishable from a header that wrote no batch at all, so the file
// that wrote `batch=0` is told it wrote nothing. A grammar refusal that misnames the
// key it is refusing sends the operator to edit a line that is already there.
func TestABatchOfZeroIsRefusedForWhatItActuallyIs(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(
		"-- pkit: phase=data\n-- pkit: batch=0\n-- pkit: table=probe\nUPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)"))
	if err == nil {
		t.Fatal("batch=0 applied: a window of no rows is a drain that never advances")
	}
	message := err.Error()
	if !strings.Contains(message, "batch=0") || !strings.Contains(message, "1…100000") {
		t.Errorf("the refusal %q does not name batch=0 and the domain it is outside; batch=100001 is refused with both, and 0 is the value the parser reads as absent", message)
	}
}
