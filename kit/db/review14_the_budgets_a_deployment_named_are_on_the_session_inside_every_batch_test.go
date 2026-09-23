package db_test

// review14_the_budgets_a_deployment_named_are_on_the_session_inside_every_batch_test.go
// pins the sentence at the top of the migrations section of `kit/db/README.md`:
//
//	"The same two values go on the session before every batch of a drain (`BackfillWith`,
//	and through it `app.Drain`, `jobs.BackfillMigrations` and `platformkit migrate --drain`)…"
//
// Every existing case that reads the budgets back (`migrate_expand_contract_test.go`'s
// TestMigrationsRunInsideTheStatementBudgets, `kit/app`'s
// TestTheMigrationBudgetsComeFromConfiguration) asks the question of a *schema* file, which
// is the half the budgets were written for. The drain is the half the sentence is about —
// fifty transactions of waiting for rows the running application holds, which is why
// `drain` re-asserts `runner.budgets` inside its loop rather than once — and until this case
// nothing ran a batch under a budget an installation had named to see whether the batch saw
// it. A drain that ran on the defaults while the deployment believed it had shortened the
// wait would be exactly the failure the README's last clause forbids ("an operator who
// shortens the wait because a boot must not sit on a busy table has to get it for the work,
// not only for the file beside it"), and the settings are session state, so the only honest
// reading is the one taken from inside the batch.

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheBudgetsADeploymentNamedAreOnTheSessionInsideEveryBatchOfADrain(t *testing.T) {
	lock := 900 * time.Millisecond
	statement := 4 * time.Second
	budget := db.MigrationBudget{LockTimeout: &lock, StatementTimeout: &statement}

	for _, door := range []string{"MigrateWith", "BackfillWith"} {
		t.Run(door, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_seed.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);
CREATE TABLE budgets (setting text NOT NULL, value text NOT NULL);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 12) g`)},
			}
			source := db.MigrationSource{Owner: "budgetdoor", Files: files}
			if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
				t.Fatal(err)
			}
			// One statement, and it reads the window, so the drain wraps it and runs three
			// of them over twelve rows at batch=5. Each row it touches records what the two
			// settings said at that moment.
			files["000002_record.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
INSERT INTO budgets (setting, value)
SELECT s, current_setting(s) FROM batch CROSS JOIN (VALUES ('lock_timeout'), ('statement_timeout')) v (s)`)}

			var err error
			switch door {
			case "MigrateWith":
				err = db.MigrateWith(t.Context(), migrateURL, budget, source)
			case "BackfillWith":
				err = db.BackfillWith(t.Context(), migrateURL, budget, source)
			}
			if err != nil {
				t.Fatalf("%s: %v", door, err)
			}
			admin := dbtest.Open(t, migrateURL)

			// Twelve rows, two settings each: the batch count is not the point, but a run
			// that recorded nothing ran no window at all and could not answer the question.
			if n := countRows(t, admin, "SELECT count(*) FROM budgets"); n != 24 {
				t.Fatalf("%d rows record the settings, want 24 (twelve rows x two settings) — the drain wrote %d windows", n, n/2)
			}
			for _, want := range []struct{ setting, value string }{
				{"lock_timeout", "900ms"},
				{"statement_timeout", "4s"},
			} {
				n := countRows(t, admin, "SELECT count(*) FROM budgets WHERE setting = "+
					quoteLiteral(want.setting)+" AND value = "+quoteLiteral(want.value))
				if n != 12 {
					var other string
					scan(t, admin, "SELECT DISTINCT value FROM budgets WHERE setting = "+
						quoteLiteral(want.setting), &other)
					t.Errorf("%d of the 12 batch readings of %s said %q; the drain ran a batch on a budget its deployment did not name (values seen: %s)",
						n, want.setting, want.value, other)
				}
			}
		})
	}
}
