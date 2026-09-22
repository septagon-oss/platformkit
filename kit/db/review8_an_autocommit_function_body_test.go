package db_test

// review8_an_autocommit_function_body_test.go is the tenth round's case for how far the
// first finding's principle reaches past the two refusals it was reported against.
//
// The principle the round adopted is that a refusal no `allow=` can reach may not be decided
// from a reading that is wrong about where a value ends — because the file it refuses is not
// correctable: no marker excepts it, and the remedy its sentence names cannot be carried out.
// Three refusals have no marker and read a statement split: the executor's "a data file is one
// statement", `data-with-ddl`, and this one. `index-concurrent-without-autocommit` and
// `autocommit-without-concurrently` ask their question of the whole body and so never consult
// a split; the rules that read a dollar body from the inside on purpose all carry a marker.
//
// `autocommit-not-rerunnable` refuses an autocommit file whose own statement cannot survive a
// second run — `CREATE INDEX CONCURRENTLY` without `IF NOT EXISTS`, or `DROP INDEX
// CONCURRENTLY` without `IF EXISTS`. Read from inside a value, it refused a file that was
// *writing* a function body whose statements mention CONCURRENTLY, and said the file's own
// statement could not be re-run when the statement it was talking about is data. Measured
// against the server (psql, this repository's own cluster):
//
//	ERROR:  CREATE INDEX CONCURRENTLY cannot be executed from a function
//	ERROR:  DROP INDEX CONCURRENTLY cannot be executed from a function
//
// so a statement list inside a value of an autocommit file cannot be the unrerunnable
// statement the rule is about even if the guard reads it as one. The leg below fails before
// the change for that reason — the split cuts at the semicolon after the `DELETE`, and the
// piece that begins `create index concurrently` is read as the file's own statement — and the
// control beside it, a file whose own statement is the plain `CREATE INDEX CONCURRENTLY`, is
// refused exactly as it always was, which is the leg that would catch a round that answered the
// false positive by taking the rule's teeth away.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAnAutocommitFileMayWriteAFunctionThatMentionsConcurrently(t *testing.T) {
	const seed = `CREATE TABLE probe (id bigint PRIMARY KEY, t text)`
	for _, tc := range []struct {
		name, file string
		// refused names the rule the run has to answer with; "" is the run that applies.
		refused string
	}{
		{
			name: "the file writes a function whose body builds an index, which is not the file's own statement",
			file: "-- pkit: autocommit=true\n" +
				"CREATE FUNCTION probe_reindex() RETURNS void AS $body$\n" +
				"BEGIN\n" +
				"  DELETE FROM probe WHERE id < 0;\n" +
				"  CREATE INDEX CONCURRENTLY probe_t_idx ON probe (t);\n" +
				"END\n" +
				"$body$ LANGUAGE plpgsql",
			refused: "",
		},
		{
			name:    "the file's own statement is the build that cannot be repeated",
			file:    "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY probe_t_idx ON probe (t)",
			refused: "autocommit-not-rerunnable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql":   {Data: []byte(seed)},
				"000002_reindex.up.sql": {Data: []byte(tc.file)},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fnbody", Files: files})
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) {
					t.Fatalf("the autocommit file whose own statement cannot be re-run was not refused by %s: %v", tc.refused, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the run refused an autocommit file for the statements of a function body it writes: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM pg_proc WHERE proname = 'probe_reindex'"); n != 1 {
				t.Errorf("%d functions named probe_reindex: the file did not apply", n)
			}
			// The body's own statement stayed the body's: creating the function is the
			// release, and the build it describes happens when something calls it.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_indexes WHERE indexname = 'probe_t_idx'"); n != 0 {
				t.Errorf("%d indexes named probe_t_idx: the guard read the file's value as the statement it will run", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'fnbody'"); n != 2 {
				t.Errorf("%d history rows, want both files", n)
			}
		})
	}
}
