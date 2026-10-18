package dbtest

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if record := os.Getenv("PLATFORMKIT_TEMPLATE_FAILURE_RECORD"); record != "" {
		os.Exit(RunInOwnDatabaseWithTemplate(m, func(adminURL, _ string) error {
			target, err := url.Parse(adminURL)
			if err != nil {
				return err
			}
			if err := os.WriteFile(record, []byte(strings.TrimPrefix(target.Path, "/")), 0o600); err != nil {
				return err
			}
			return errors.New("the installation was refused")
		}))
	}
	os.Exit(m.Run())
}

func TestARefusedTemplatePreparationLeavesNoDatabase(t *testing.T) {
	adminURL := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	_ = mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	record := t.TempDir() + "/template-database"
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^$")
	command.Env = append(os.Environ(), "PLATFORMKIT_TEMPLATE_FAILURE_RECORD="+record)
	output, err := command.CombinedOutput()
	if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 {
		t.Fatalf("refused preparation returned %v, want exit 1: %s", err, output)
	}
	if !strings.Contains(string(output), "the installation was refused") {
		t.Fatalf("preparation lost its refusal reason: %s", output)
	}
	name, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the preparation did not record the database it reached: %v", err)
	}
	t.Cleanup(func() {
		if err := removeOwnDatabase(adminURL, string(name)); err != nil {
			t.Errorf("remove the preparation's database: %v", err)
		}
	})
	admin := Open(t, adminURL)
	var remains bool
	if err := admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", string(name)).Scan(&remains); err != nil {
		t.Fatal(err)
	}
	if remains {
		t.Errorf("refused preparation left database %s behind; package teardown must remove its template on failure", name)
	}
}
