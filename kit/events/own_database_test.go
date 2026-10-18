package events_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// Durable advisory locks are keyed per database, so the events cases reach a
// database no other package's cases share; a case that lands in the shared one
// contends with every other package that delivers under the same durable name.
func TestEventsCasesRunInADatabaseOfTheirOwn(t *testing.T) {
	owner, _ := dbtest.Schema(t)
	var name string
	if err := owner.QueryRowContext(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "platformkit_test_") {
		t.Fatalf("events cases run in database %q, want one of their own (platformkit_test_*)", name)
	}
}
