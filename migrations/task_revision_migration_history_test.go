package migrations_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAnInstallationWithTaskRevisionsKeepsItsAppliedMigrationHistory(t *testing.T) {
	previous := appliedModuleSources(t, "task", "000050", "000050_task_revision.up.sql")
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, previous...); err != nil {
		t.Fatalf("install the previously numbered task revision migration: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	id, tenant := uuid.New(), uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO tasks (id, tenant_id, title, revision) VALUES ($1, $2, 'retained task', 7)", id, tenant); err != nil {
		t.Fatal(err)
	}
	var applied string
	if err := admin.QueryRowContext(t.Context(),
		"SELECT applied_at::text FROM schema_migrations WHERE owner = 'task' AND version = 50").Scan(&applied); err != nil {
		t.Fatalf("reach an installation that applied the previous task revision version: %v", err)
	}
	if err := db.Migrate(t.Context(), adminURL, everything...); err != nil {
		t.Fatalf("upgrade must preserve an installation that already applied task revisions: %v", err)
	}
	var revision int64
	if err := admin.QueryRowContext(t.Context(),
		"SELECT revision FROM tasks WHERE id = $1 AND tenant_id = $2", id, tenant).Scan(&revision); err != nil || revision != 7 {
		t.Errorf("upgrade lost the task's revision: revision=%d, error=%v", revision, err)
	}
	var after string
	if err := admin.QueryRowContext(t.Context(),
		"SELECT applied_at::text FROM schema_migrations WHERE owner = 'task' AND version = 50").Scan(&after); err != nil || after != applied {
		t.Errorf("upgrade replaced applied migration history: before=%q after=%q, error=%v", applied, after, err)
	}
}
