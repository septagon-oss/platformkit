package db_test

// review3_rule_reads_the_statement_test.go is the third review's case for the
// one rule the second review's finding 2 did not reach.
//
// That finding was that `alter-column-type` matched a keyword rather than the
// operation, and the fix moved the predicate inside a single statement
// (`rewritesAColumnType` walks `f.statements` and requires both halves in one of
// them). `add-column-not-null` was left reading the whole body: it fires when
// ADD COLUMN, NOT NULL and the absence of DEFAULT agree over the file. A file
// with two statements therefore answers the rule with a word that belongs to the
// other statement — `ALTER COLUMN … SET DEFAULT` is about a different column
// entirely — and the rewrite the rule exists to stop is applied with no refusal
// and, because nothing fired, with no `allow=` and no reason for a reviewer to
// read either.
//
// migrations/README.md states the rule as firing on "`ADD COLUMN … NOT NULL`
// with no `DEFAULT`", which is a statement about one column definition, and the
// same README says the rules "read operations rather than spellings". Both say
// the case below is refused.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestANotNullColumnIsRefusedForItsOwnStatement(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(
		"ALTER TABLE probe ADD COLUMN c integer NOT NULL;\nALTER TABLE probe ALTER COLUMN a SET DEFAULT 'none'"))
	if err == nil {
		admin := dbtest.Open(t, migrateURL)
		t.Fatalf("a file that adds a NOT NULL column with no default was accepted because another statement in the same file says DEFAULT somewhere: %d rows now carry the column that rewrites the table under ACCESS EXCLUSIVE",
			countRows(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'probe'"+
				" AND table_schema = current_schema() AND column_name = 'c'"))
	}
	if !strings.Contains(err.Error(), "add-column-not-null") {
		t.Errorf("the refusal %q names a rule other than add-column-not-null, which is the one the file breaks", err)
	}
	// The remedy the rule names is a statement-level one: the same ADD COLUMN
	// with its own DEFAULT is ordinary SQL.
	if err := db.Migrate(t.Context(), migrateURL, reviewProbeSource(
		"ALTER TABLE probe ADD COLUMN c integer NOT NULL DEFAULT 0;\nALTER TABLE probe ALTER COLUMN a SET DEFAULT 'none'")); err != nil {
		t.Errorf("the same file with a default on the column being added was refused: %v", err)
	}
}
