package db_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// A refused window binding can be corrected to select either half of the rows.
// Both halves contain six rows, so counting updates alone cannot distinguish them.
func TestWindowBindingCorrectionWritesOnlyTheSelectedRows(t *testing.T) {
	for _, membership := range []string{"IN", "NOT IN"} {
		t.Run(membership, func(t *testing.T) {
			url, _ := dbtest.URLs(t)
			files := windowShapesFiles(`WITH batch (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`)
			source := db.MigrationSource{Owner: "selection", Files: files}
			if err := db.Migrate(t.Context(), url, source); err == nil || !strings.Contains(err.Error(), "the window is the relation named batch") {
				t.Fatalf("window binding refusal: %v", err)
			}
			admin := dbtest.Open(t, url)
			for _, query := range []string{
				"SELECT count(*) FROM probe WHERE note <> ''",
				"SELECT count(*) FROM schema_migration_backfill",
				"SELECT count(*) FROM schema_migrations WHERE owner = 'selection' AND version = 2",
			} {
				if n := countRows(t, admin, query); n != 0 {
					t.Errorf("refusal left %d rows: %s", n, query)
				}
			}
			files["000002_fill.up.sql"] = windowShapesData(`WITH selected (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id ` + membership + ` (SELECT id FROM selected)`)
			if err := db.Migrate(t.Context(), url, source); err != nil {
				t.Fatal(err)
			}
			condition := "id < 7"
			if membership == "NOT IN" {
				condition = "id >= 7"
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE (note = 'done') IS DISTINCT FROM ("+condition+")"); n != 0 {
				t.Errorf("correction wrote the wrong selection for %d rows", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("correction left %d progress rows", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'selection' AND version = 2"); n != 1 {
				t.Errorf("correction recorded %d versions, want 1", n)
			}
		})
	}
}
