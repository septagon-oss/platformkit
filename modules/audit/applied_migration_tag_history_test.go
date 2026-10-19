package audit_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppliedMigrationGateIgnoresReleasesOutsideTheBaseHistory(t *testing.T) {
	for _, branch := range []string{"candidate", "another-branch"} {
		t.Run(branch, func(t *testing.T) {
			const path = "migrations/000001_history.up.sql"
			dir, script, base := migrationGateRepository(t, path)
			if branch == "another-branch" {
				migrationGateGit(t, dir, "checkout", "-q", "-b", "other", base+"^")
				if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			migrationGateWrite(t, dir, path, "CREATE TABLE history (id int);\n-- Changed after shipping.\n")
			migrationGateCommit(t, dir, "candidate schema")
			if branch == "candidate" {
				assertMigrationGateRefuses(t, dir, script, base, path)
			}
			migrationGateGit(t, dir, "tag", "v0.2.0")
			if branch == "another-branch" {
				migrationGateGit(t, dir, "checkout", "-q", "main")
				migrationGateWrite(t, dir, path, "CREATE TABLE history (id int);\n-- Changed after shipping.\n")
				migrationGateCommit(t, dir, "edit an applied migration")
			}
			assertMigrationGateRefuses(t, dir, script, base, path)
		})
	}
}

// migrationGateRepository creates a released repository followed by a shipped migration
// no release yet contains, so the base commit determines the installation's applied bytes.
func migrationGateRepository(t *testing.T, path string) (dir, script, base string) {
	t.Helper()
	script, err := filepath.Abs("../../scripts/check_applied_migrations.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	migrationGateGit(t, dir, "init", "-q", "-b", "main")
	migrationGateGit(t, dir, "config", "core.quotePath", "true")
	migrationGateWrite(t, dir, "README.md", "Schema fixture.\n")
	migrationGateCommit(t, dir, "initial release")
	migrationGateGit(t, dir, "tag", "v0.1.0")
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o700); err != nil {
		t.Fatal(err)
	}
	migrationGateWrite(t, dir, path, "CREATE TABLE history (id int);\n")
	migrationGateCommit(t, dir, "shipped migration")
	base = migrationGateGit(t, dir, "rev-parse", "HEAD")
	cmd := exec.CommandContext(t.Context(), "bash", script, base)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unchanged applied migration must pass: %v\n%s", err, out)
	}
	return dir, script, base
}

func migrationGateGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func migrationGateWrite(t *testing.T, dir, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func migrationGateCommit(t *testing.T, dir, message string) {
	t.Helper()
	migrationGateGit(t, dir, "add", "-A")
	migrationGateGit(t, dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", message)
}

func assertMigrationGateRefuses(t *testing.T, dir, script, base, path string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", script, base)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	refusal, refused := errors.AsType[*exec.ExitError](err)
	if !refused || refusal.ExitCode() != 1 {
		t.Fatalf("an edited applied migration must be refused; got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), path+" changed after it was applied; add a new migration") {
		t.Errorf("refusal must name the file and cure: %s", out)
	}
}
