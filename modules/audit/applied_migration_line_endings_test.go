package audit_test

import "testing"

func TestAppliedMigrationGateRefusesChangedWorkingTreeLineEndings(t *testing.T) {
	const path = "migrations/000001_history.up.sql"
	dir, script, base := migrationGateRepository(t, path)
	migrationGateGit(t, dir, "tag", "v0.2.0")
	migrationGateGit(t, dir, "config", "core.autocrlf", "true")
	const original = "CREATE TABLE history (id int);\n"
	const changed = "CREATE TABLE history (id int);\r\n"
	assertMigrationBytesAreIncompatible(t, path, original, changed)
	migrationGateWrite(t, dir, path, changed)
	assertMigrationGateRefuses(t, dir, script, base, path)
}
