package db

import "testing"

// The zero MigrateOptions is documented as the run Migrate and Backfill perform, which
// is only true because the empty budget it carries reaches the same two settings the
// documented default does: a field that steered the statement away from the default
// would show up here before it changed what every installation waits for.
func TestTheZeroBudgetAndTheDefaultBudgetIssueTheSameSettings(t *testing.T) {
	want := DefaultMigrationBudget().statement()
	for name, got := range map[string]string{
		"a zero MigrationBudget":  MigrationBudget{}.statement(),
		"the zero MigrateOptions": MigrateOptions{}.Budget.statement(),
	} {
		if got != want {
			t.Errorf("%s puts %q on the session, the documented default puts %q", name, got, want)
		}
	}
}
