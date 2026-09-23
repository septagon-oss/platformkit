package app

// review3_budget_from_configuration_test.go pins the one line of the migration
// story that no other case walks: kit/db/README.md says
// "database.lock_timeout and database.statement_timeout change them", config.example
// .yaml documents both, and kit/config carries them as optional fields. Every other
// budget case hands a db.MigrationBudget straight to MigrateWith or BackfillWith
// (kit/db/review_guarantees_test.go, kit/db/backfill_budget_test.go), so a mapping
// that dropped the configured value on its way to the runner — the defaults left in
// place — would keep every one of them green while an operator's configuration did
// nothing. This is the case that reads the value back out of the session the
// migration actually ran in.

import (
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/module"
	"testing"
	"testing/fstest"
	"time"
)

func TestTheMigrationBudgetFromTheConfigurationReachesTheFile(t *testing.T) {
	cfg, _ := compose(t)
	lock := 900 * time.Millisecond
	statement := 4 * time.Second
	cfg.Database.LockTimeout = &lock
	cfg.Database.StatementTimeout = &statement
	owner := dbtest.Open(t, cfg.Database.MigrateURL)

	mod := module.Module{Name: "budgetmod", Migrations: fstest.MapFS{
		"000001_read.up.sql": {Data: []byte(`CREATE TABLE budgets (setting text PRIMARY KEY, value text);
INSERT INTO budgets VALUES ('lock_timeout', current_setting('lock_timeout'));
INSERT INTO budgets VALUES ('statement_timeout', current_setting('statement_timeout'))`)},
	}}
	if err := Migrate(t.Context(), cfg, []module.Module{mod}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A file that sets a budget for itself must not be the way a deployment sets
	// one for the release, so the value asserted is the one in force when the
	// file's own statements ran.
	for setting, want := range map[string]string{"lock_timeout": "900ms", "statement_timeout": "4s"} {
		var got string
		if err := owner.QueryRowContext(t.Context(),
			"SELECT value FROM budgets WHERE setting = "+quote(setting)).Scan(&got); err != nil {
			t.Fatalf("reading %s back from the migration's own session: %v", setting, err)
		}
		if got != want {
			t.Errorf("%s during the migration = %q, want the %q the configuration named; kit/db's default would read %q",
				setting, got, want, map[string]string{"lock_timeout": "5s", "statement_timeout": "0"}[setting])
		}
	}

	// The drain's batches re-assert the same two values (kit/db/backfill_budget_test
	// go puts a configured budget on a batch by holding a row the first window is
	// about to write); what nothing else walked is the path from a parsed
	// configuration to the session a file runs on, which is the half above.
}

func quote(literal string) string { return "'" + literal + "'" }
