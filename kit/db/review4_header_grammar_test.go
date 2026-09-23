package db_test

// review4_header_grammar_test.go is the fourth review's case for a header the
// grammar accepts and then reads as something other than what the file says.
//
// The specification's own grammar (SPECIFY, "The header grammar") states the rule
// this case holds:
//
//	"A `reason=` value runs to the end of its line and may contain spaces, so it
//	must be the last pair on that line."
//
// "must" is the domain, and the domain is what `parseHeader` refuses everything
// else outside of — every other key whose value is out of range, every repeated
// key, every unknown key, each with a message naming the key and its legal
// domain. Here the domain is unenforced: the parser gives `reason=` the rest of
// the line whatever is written there, so a `-- pkit:` pair that follows it is not
// a declaration any more, and nothing says so. kit/db/migration_header.go's own
// reason for refusing an unknown key is the harm this leaves:
//
//	"a marker the runner would not read says 'this file was reviewed' about a file
//	 that was not."
//
// The file below is that harm in its worst shape: the unread pair is
// `phase=contract expand=2`, and the statement is a `DROP COLUMN` — the one
// statement the `drop-column` rule exists to move into the release after the
// expansion that replaced it. Read as written, the file is a contract half whose
// expansion never came, and the runner refuses it. Read as the parser reads it,
// the file is an ordinary expand file — and the swallow makes it *self-consistent*,
// because eating the phase is exactly what makes `drop-column` fire, so the
// `allow=drop-column` on the same line is a used exception rather than an unused
// one, `unused-allow` has nothing to report, and the release ships a dropped column
// beside the code still reading it. The marker says a reviewer read a contract half
// that was never declared.
//
// The other two cases in this file are pins rather than complaints, and each marks
// the boundary of what was found. The autocommit shape the rule table points at
// (`autocommit=true` for a concurrent build) is a *two*-statement file, which
// SPECIFY records as a measured decision — "A file with more than one statement in
// this mode is not caught by a parser: … The message is Postgres', the rule is
// ours". What is pinned here is the half a release depends on: the file that never
// ran leaves no index and no ledger row, so the next run is the same file again.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestAHeaderKeyAfterReasonIsNotSilentlyUnread.
func TestAHeaderKeyAfterReasonIsNotSilentlyUnread(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, c integer NOT NULL DEFAULT 0)")},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "swallow", Files: files}); err != nil {
		t.Fatalf("the owner's schema file: %v", err)
	}
	files["000002_drop.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: allow=drop-column reason=the release after phase=contract expand=2
ALTER TABLE probe DROP COLUMN c`)}

	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "swallow", Files: files})
	if err == nil {
		t.Errorf("the file whose header reads `phase=contract expand=2` applied with no refusal and named no rule: %v", err)
	}
	// The column is the assertion, and it does not care which refusal the remedy
	// is: a `DROP COLUMN` whose header declares a contract half has to be read, not
	// run. Refusing the line (what SPECIFY's "must be the last pair" implies) and
	// reading the pair (which leaves the file waiting for an expansion that never
	// applied) both land here.
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'c' AND NOT attisdropped"); n != 1 {
		t.Errorf("the column a header declares contract-half is gone (%d present): a `-- pkit:` pair after `reason=` is never read, and the file applied as an expand file with allow=drop-column marked on it", n)
	}
}

// TestADataPhaseWrittenAfterReasonIsCaughtByTheMarkerNotThePhase is the boundary
// of the same swallow, and it is a pin rather than a complaint: when the eaten pair
// is `phase=data batch= table=`, the exception the author wrote on that line stops
// being one (`data-body-unbounded` fires only on a data file) and `unused-allow`
// refuses the run — so the whole-table-in-one-transaction execution this shape
// would otherwise have run does not happen.
//
// What it records is how narrow the finding above is, and what the operator is told:
// a refusal that names the marker rather than the phase that was eaten.
func TestADataPhaseWrittenAfterReasonIsCaughtByTheMarkerNotThePhase(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 30) g")},
		"000002_mark.up.sql": {Data: []byte(`-- pkit: allow=data-body-unbounded reason=in the release after this one phase=data batch=4 table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "swalowndata", Files: files})
	if err == nil {
		t.Fatal("a data file whose `phase=data batch= table=` were eaten by `reason=` applied with no refusal; the whole-table UPDATE ran as one transactional statement")
	}
	// The refusal is `unused-allow`, before the runner connects: with the phase
	// eaten the file is an expand file, `data-body-unbounded` never fires, and the
	// exception on the line becomes one nobody needed. The operator is sent to the
	// marker; nothing here says a phase was swallowed, and no table was created.
	if !strings.Contains(err.Error(), "unused-allow") || !strings.Contains(err.Error(), "allow=data-body-unbounded") {
		t.Errorf("the run was refused by something other than the marker: %v", err)
	}
}

// TestAnAutocommitFileThatDidNotRunLeavesNothingBehind pins the state half of the
// shape SPECIFY accepts: the two-statement autocommit file is refused by PostgreSQL
// (error 25001) rather than by a rule, and the delivery's reasoning for that is
// recorded in the specification. What has to be true, and is, is that a file which
// did not run wrote nothing down — no index, no ledger row — so the operator's next
// run is the same file and not a repair.
//
// migrations/README.md's `autocommit` row still reads "the file's one statement runs
// with no transaction around it" and its `index-concurrent-without-autocommit` row
// offers `autocommit=true` flat as the remedy; that the *second* statement in such a
// file is answered in PostgreSQL's vocabulary is a decision a reader of the
// committed pages cannot see, and is the one sentence worth adding there.
func TestAnAutocommitFileThatDidNotRunLeavesNothingBehind(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a integer NOT NULL DEFAULT 0)")},
		"000002_two_idx.up.sql": {Data: []byte(`-- pkit: autocommit=true
CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_a_idx ON probe (a);
CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_id_idx ON probe (id)`)},
	}
	source := db.MigrationSource{Owner: "acmt", Files: files}
	err := db.Migrate(t.Context(), migrateURL, source)
	admin := dbtest.Open(t, migrateURL)
	if err == nil {
		// The alternative honest answer: both statements ran outside a transaction.
		if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname IN ('probe_a_idx','probe_id_idx')"+
			" AND relnamespace = current_schema()::regnamespace"); n != 2 {
			t.Errorf("the autocommit file reported success and %d of its 2 indexes exist", n)
		}
		return
	}
	if !strings.Contains(err.Error(), "acmt/000002_two_idx.up.sql") {
		t.Errorf("the refusal %q does not name the file it refused", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'acmt' AND version = 2"); n != 0 {
		t.Errorf("a file that did not run wrote %d history rows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname IN ('probe_a_idx','probe_id_idx')"+
		" AND relnamespace = current_schema()::regnamespace"); n != 0 {
		t.Errorf("a file that did not run left %d index(es) behind", n)
	}
	// And the file is still pending afterwards: the same run, corrected to one
	// statement, applies.
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'acmt'"); n != 1 {
		t.Errorf("%d history rows for an owner whose second file never ran", n)
	}
	// The retry is the same file, one statement, and it applies.
	files["000002_two_idx.up.sql"].Data = []byte(`-- pkit: autocommit=true
CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_a_idx ON probe (a)`)
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the corrected file: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe_a_idx'"+
		" AND relnamespace = current_schema()::regnamespace"); n != 1 {
		t.Errorf("the corrected autocommit file left %d indexes behind", n)
	}
}
