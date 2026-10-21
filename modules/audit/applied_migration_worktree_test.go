package audit_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppliedMigrationGateRefusesLocalCommentChanges(t *testing.T) {
	script, err := filepath.Abs("../../scripts/check_applied_migrations.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, staged := range []bool{false, true} {
		name := "working-tree"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), args[0], args[1:]...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run("git", "init", "-q", "-b", "main")
			if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o700); err != nil {
				t.Fatal(err)
			}
			const path = "migrations/000001_history.up.sql"
			const original = "CREATE TABLE history (id int);\n"
			write(path, original)
			run("git", "add", ".")
			run("git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "schema")
			run("git", "tag", "v0.1.0")
			base := run("git", "rev-parse", "HEAD")
			write("README.md", "Application documentation.\n")
			run("git", "add", ".")
			run("git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "documentation")
			run("bash", script, base)
			write("migrations/000002_next.up.sql", "CREATE TABLE next (id int);\n")
			run("bash", script, base)
			write(path, original+"-- Explain the existing table.\n")
			if staged {
				run("git", "add", path)
			}
			cmd := exec.CommandContext(t.Context(), "bash", script, base)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			refusal, refused := errors.AsType[*exec.ExitError](err)
			if !refused || refusal.ExitCode() != 1 {
				t.Fatalf("an edited applied migration must be refused before commit; got %v\n%s", err, out)
			}
			if !strings.Contains(string(out), path+" changed after it was applied; add a new migration") {
				t.Errorf("refusal must name the file and cure: %s", out)
			}
		})
	}
}
