package db_test

// review6_index_target_test.go is the seventh review's case for what the
// index-not-concurrent rule decides from.
//
// migrations/README.md: "`index-not-concurrent` | `CREATE INDEX` without
// `CONCURRENTLY` on a table this file does not create | the plain build takes a
// `SHARE` lock that stops every writer for the length of the build", and the rule's
// own comment in kit/db/migration_rules.go gives the reason for the exemption: "the
// file that creates a table may index it, because nothing is reading it yet, and that
// is every module's first file. Anything else is a build on a table with readers."
//
// `created` is filled from the file's own text, and it is filled the same way for
// `CREATE TABLE t` and `CREATE TABLE IF NOT EXISTS t` — the regexp captures the
// difference in its first group and the code drops it. `IF NOT EXISTS` is the spelling
// that says the table may already be there, which is the one case where the reason for
// the exemption is false: on an installation where the table exists, that file's
// `CREATE INDEX` is a plain build on a table with readers, which is what the rule's own
// row says stops every writer for the length of the build. The version that really
// created the table is a fact one file away, and the file is excused whatever the
// ledger says.
//
// Both legs assert through the catalogue — an index row means the build ran — and the
// first is the shape the exemption is written for, which passes today: a file that
// really does create its table may index it in the same file.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAPlainIndexBuildIsExcusedOnlyByTheFileThatCreatesTheTable(t *testing.T) {
	const seed = `CREATE TABLE probe (id bigint PRIMARY KEY, a text);
INSERT INTO probe (id, a) SELECT g, 'a' || g FROM generate_series(1,25) g`
	for _, tc := range []struct {
		name, file, table string
		exempt            bool
	}{
		{
			name:   "a file indexes the table it creates in this same file, which is the exemption",
			file:   "CREATE TABLE fresh (id bigint PRIMARY KEY, a text);\nCREATE INDEX fresh_a_idx ON fresh (a)",
			table:  "fresh_a_idx",
			exempt: true,
		},
		{
			name:   "IF NOT EXISTS says the table may already be there",
			file:   "CREATE TABLE IF NOT EXISTS probe (id bigint PRIMARY KEY, a text);\nCREATE INDEX probe_a_idx ON probe (a)",
			table:  "probe_a_idx",
			exempt: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(seed)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "indextarget", Files: files}); err != nil {
				t.Fatalf("the owner's first file: %v", err)
			}
			files["000002_index.up.sql"] = &fstest.MapFile{Data: []byte(tc.file)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "indextarget", Files: files})
			admin := dbtest.Open(t, migrateURL)
			built := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = '"+tc.table+"'") == 1
			if !tc.exempt {
				if err == nil {
					t.Errorf("the file was accepted: its plain `CREATE INDEX` is a SHARE lock over a table this installation already had, and the guard excused it because the same file said `CREATE TABLE IF NOT EXISTS`")
				} else if !strings.Contains(err.Error(), "index-not-concurrent") {
					t.Errorf("the refusal %q does not name index-not-concurrent", err)
				}
				if built {
					t.Error("the index was built: the build the rule is about ran anyway")
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'indextarget' AND version = 2"); n != 0 {
					t.Errorf("the refused run wrote %d history rows", n)
				}
				return
			}
			if err != nil {
				t.Errorf("the file that really does create its table was refused: %v\nnothing is reading a table this same file created; that is the exemption migrations/README.md states", err)
			}
			if !built {
				t.Error("the index the exemption allows was not built")
			}
		})
	}
}
