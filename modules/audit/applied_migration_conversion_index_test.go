package audit_test

import "testing"

func TestAppliedMigrationGateChecksRawBytesAndTheIndexTogether(t *testing.T) {
	const path = "migrations/000001_história.up.sql"
	const original = "CREATE TABLE history (id int);\n"
	dir, script, base := migrationGateRepository(t, path)
	migrationGateGit(t, dir, "tag", "v0.2.0")
	migrationGateGit(t, dir, "config", "core.autocrlf", "true")
	migrationGateWrite(t, dir, path, "CREATE TABLE history (id int);\r\n")
	migrationGateGit(t, dir, "add", path)
	if diff := migrationGateGit(t, dir, "diff", "HEAD", "--", path); diff != "" {
		t.Fatalf("conversion must hide the raw byte change from Git: %s", diff)
	}
	assertMigrationGateRefuses(t, dir, script, base, path)

	migrationGateWrite(t, dir, path, original)
	migrationGateGit(t, dir, "add", path)
	migrationGateGit(t, dir, "-c", "core.autocrlf=false", "diff", "--exit-code", "HEAD", "--", path)
	migrationGateWrite(t, dir, path, original+"-- Changed after shipping.\n")
	migrationGateGit(t, dir, "add", path)
	migrationGateWrite(t, dir, path, original)
	assertMigrationGateRefuses(t, dir, script, base, path)
}
