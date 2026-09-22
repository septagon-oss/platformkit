package db_test

// refusal_names_test.go is the ninth round's case for the eighth review's fourth finding.
// Two of the runner's runtime refusal ids — `data-table-missing` and
// `data-key-not-primary-key` — were written down only in the task's own specification:
// `grep -rn "data-table-missing" --include=*.go --include=*.md .` found the string in one
// reviewer's comment and nowhere else, so an operator handed the id by a log line or a
// runbook found nothing in the repository that refused them, and the id could drift from
// the sentence it stands for with no gate noticing.
//
// The fix is that the refusal prints its own id — `refusal <id>: <what was found>; <what
// to do>` — and this case reads it back off the message of a run that really refused. Each
// leg therefore asserts two things about the one sentence: the id in front of it, and the
// words that were the whole message before the id was added. Asserting only the id would
// pass a change that replaced the sentence with the id, which tells an operator less than
// they were told before; asserting only the sentence is what the repository did before this
// round, and it is what let the two names drift apart.
//
// The last leg reads this package's own README.md. The table there is the operator's copy
// of the same list, and a doc a test cannot disbelieve is a list rather than a gate: an id
// that stops being printed, or one added without writing it down, fails the leg beside it
// rather than surviving as somebody's grep.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestARuntimeRefusalNamesTheIDItsDocumentsGiveIt(t *testing.T) {
	for _, tc := range []struct {
		name, id, says string
		sentinel       error
		// prior is what the installation already has applied. Only the release rule
		// needs one: a fresh owner installs its own layout, contract half included,
		// and the drain of an owner with no history is the migration's own work.
		prior fstest.MapFS
		files fstest.MapFS
	}{
		{
			name: "a table the window cannot resolve",
			id:   "data-table-missing",
			says: "does not exist here",
			files: fstest.MapFS{
				"000001_probe.up.sql":    {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0)")},
				"000002_backfill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=elsewhere\nUPDATE elsewhere SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)")},
			},
		},
		{
			name: "a table keyed by two columns",
			id:   "data-key-not-primary-key",
			says: "no single-column primary key",
			files: fstest.MapFS{
				"000001_pairing.up.sql":  {Data: []byte("CREATE TABLE pairing (a bigint NOT NULL, b bigint NOT NULL, passes integer NOT NULL DEFAULT 0, PRIMARY KEY (a, b))")},
				"000002_backfill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=pairing\nUPDATE pairing SET passes = passes + 1 WHERE a IN (SELECT a FROM batch)")},
			},
		},
		{
			name: "a contract half beside its own expansion",
			id:   "contract-without-expansion",
			says: "waits for",
			prior: fstest.MapFS{
				"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
			},
			files: fstest.MapFS{
				"000001_orders.up.sql":     {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
				"000002_amount.up.sql":     {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
				"000003_drop_total.up.sql": {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")},
			},
		},
		{
			name:     "a drain a migration could not finish inside its own bound",
			id:       "backfill-exceeds-install-budget",
			says:     "the bound an installation gives itself",
			sentinel: db.ErrBackfillBudget,
			files: fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,60) g")},
				"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			if tc.prior != nil {
				if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "named", Files: tc.prior}); err != nil {
					t.Fatalf("the release this installation is one behind: %v", err)
				}
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "named", Files: tc.files})
			if err == nil {
				t.Fatalf("the run that must refuse %s applied the files instead", tc.id)
			}
			if !strings.Contains(err.Error(), "refusal "+tc.id) {
				t.Errorf("the refusal %q does not print its id (%s): an operator grep of this repository finds the sentence, the id, or neither, and only the pair leads from a log line to the code that refused", err, tc.id)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal %q no longer says %q: naming a refusal is not the same thing as explaining one", err, tc.says)
			}
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Errorf("the refusal %q is not the sentinel an operator's script matches with errors.Is", err)
			}
		})
	}
}

// TestTheRuntimeRefusalIDsAreTheOnesTheRunnerDocuments reads the operator's copy of the
// list out of this package's own README.md, beside the legs above that print each id. One
// without the other is the finding: a table that names an id nothing prints, or a sentence
// that prints one nothing names.
func TestTheRuntimeRefusalIDsAreTheOnesTheRunnerDocuments(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this package's own README.md: %v", err)
	}
	for _, id := range []string{
		"data-table-missing", "data-key-not-primary-key",
		"contract-without-expansion", "backfill-exceeds-install-budget",
	} {
		if !strings.Contains(string(readme), "`"+id+"`") {
			t.Errorf("README.md never names the refusal id %s, which a run prints: the table an operator reads and the sentence that refused them have to be one list", id)
		}
	}
}
