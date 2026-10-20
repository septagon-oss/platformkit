package audit_test

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAppliedMigrationGateRefusesChangesToQuotedPaths(t *testing.T) {
	const path = "migrations/000001_história.up.sql"
	dir, script, base := migrationGateRepository(t, path)
	migrationGateGit(t, dir, "tag", "v0.2.0")
	const original = "CREATE TABLE history (id int);\n"
	const changed = original + "-- Changed after shipping.\n"
	assertMigrationBytesAreIncompatible(t, path, original, changed)
	migrationGateWrite(t, dir, path, changed)
	migrationGateCommit(t, dir, "edit an applied migration")
	assertMigrationGateRefuses(t, dir, script, base, path)
}

func assertMigrationBytesAreIncompatible(t *testing.T, path, original, changed string) {
	t.Helper()
	source := db.MigrationSource{
		Owner:     "history",
		Files:     fstest.MapFS{filepath.Base(path): &fstest.MapFile{Data: []byte(original)}},
		RulesFrom: 2,
	}
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, source); err != nil {
		t.Fatalf("the migrator must accept the original filename and SQL: %v", err)
	}
	source.Files = fstest.MapFS{filepath.Base(path): &fstest.MapFile{Data: []byte(changed)}}
	if err := db.Migrate(t.Context(), adminURL, source); err == nil || !strings.Contains(err.Error(), "changed after it was applied; add a new migration") {
		t.Fatalf("editing these applied bytes must break the installation's checksum: %v", err)
	}
}
