package db_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestARunThatReturnsHandsTheCompositionKeyBack asks the property migrate.go now leaves
// to the session's end rather than to an unlock statement: whichever way Migrate
// returns — every file applied, or a file refused — no session of the run still holds
// the composition key a moment later. The read is of this test's own schema's sessions
// (holdsCompositionKey), so another package's queue cannot spend the bound. The bound
// exists because the release is now the backend's exit, which the server finishes after
// Migrate has returned; five seconds is three orders above the few milliseconds that
// teardown takes, and a run that kept its session would hold the key forever.
func TestARunThatReturnsHandsTheCompositionKeyBack(t *testing.T) {
	for _, c := range []struct {
		name    string
		body    string
		refused bool
	}{
		{"applied", "CREATE TABLE handed_back (value int)", false},
		{"refused", "CREATE TABLE handed_back (value int); SELECT 1/0", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			admin := dbtest.Open(t, migrateURL)
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "handback", Files: fstest.MapFS{
				"1_handed_back.up.sql": {Data: []byte(c.body)},
			}})
			if (err != nil) != c.refused {
				t.Fatalf("Migrate returned %v; refused=%v was the file's point", err, c.refused)
			}
			if why := waitUntil(t, 5*time.Second, nil, "the run's sessions gave the composition key back", func() bool {
				return !holdsCompositionKey(t, admin)
			}); why != "" {
				t.Fatalf("after Migrate returned (%v): %s", err, why)
			}
		})
	}
}
