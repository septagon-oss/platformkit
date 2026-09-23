package db_test

// review6_drain_refusals_test.go walks the two drain refusals the specification's own
// refusal table names — SPECIFY.md's `data-table-missing` and `data-key-not-primary-key`
// ("the `table=` name does not resolve in this database"; "the table has no
// single-column primary key", both "correctable") — which are implemented in
// kit/db/backfill.go (`primaryKey`, `tableExistsSQL`) and are not reached by any case in
// the repository: `grep -rn "single-column" kit/db/*_test.go` finds them only in comments
// describing a table that does have one.
//
// They are in the brief's deliverable 5 ("the guards, the refusals, …"), and the shape
// they refuse is the ordinary mistake — a junction table keyed by two columns, or a
// `table=` for an owner nobody selected. A branch no case walks is a branch that can
// stop firing with nothing noticed, and the honest answer about a table the window cannot
// walk is a refusal that names the key it could not find, not a drain that invents one.
//
// Both legs assert the refusal and that nothing was written; neither needs the branch's
// wording to be reached, since the ledger and the rows say whether a drain ran.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheDrainRefusesATableItCannotWindow(t *testing.T) {
	for _, tc := range []struct {
		name, schema, table, want string
	}{
		{
			name:   "a table keyed by two columns",
			schema: "CREATE TABLE pairing (a bigint NOT NULL, b bigint NOT NULL, passes integer NOT NULL DEFAULT 0, PRIMARY KEY (a, b))",
			table:  "pairing",
			want:   "no single-column primary key",
		},
		{
			name:   "a table no selected owner creates",
			schema: "CREATE TABLE probe (id bigint PRIMARY KEY, a text)",
			table:  "elsewhere",
			want:   "does not exist here",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(tc.schema)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "drainrefusal", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=" + tc.table + `
UPDATE ` + tc.table + ` SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "drainrefusal", Files: files})
			if err == nil {
				t.Error("a data file whose table the window cannot run over applied instead of refusing")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal %q does not say %s", err, tc.want)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'drainrefusal' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill WHERE owner = 'drainrefusal' AND version = 2"); n != 0 {
				t.Errorf("the refused run left %d progress rows behind: a drain that never started is now one a worker resumes", n)
			}
		})
	}
}
