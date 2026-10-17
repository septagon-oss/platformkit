package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// The operation receipt, the proposal's origin and the audit trail's origin columns
// ship as four files whose versions are read from their names, so a renumbering keeps
// this case true. Each is recorded in the ledger at that version, and the columns and
// index they add exist after the composed migrate.
func TestEveryOperationMigrationIsAppliedAtTheVersionItsFileNames(t *testing.T) {
	migrateURL, appURL := dbtest.URLs(t)
	owner := dbtest.Open(t, migrateURL)
	t.Cleanup(func() { owner.Close() })
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := fmt.Sprintf("server:\n  addr: \":0\"\n  public_host: \"rehearse.localhost\"\ndatabase:\n  url: %q\n  migrate_url: %q\nnats:\n  url: \"nats://127.0.0.1:4222\"\nlog:\n  level: \"info\"\nwebhook:\n  secret_key: %q\n", appURL, migrateURL, webhookKey)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := migrate([]string{"--config", path}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, source := range [][2]string{
		{"../../migrations", "operations"},
		{"../../modules/change/migrations", "change_operation"},
		{"../../modules/audit/migrations", "audit_operations"},
		{"../../modules/audit/migrations", "audit_operation_index"},
	} {
		dir, name := source[0], source[1]
		matches, err := filepath.Glob(filepath.Join(dir, "*_"+name+".up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		var files []string
		for _, f := range matches {
			if strings.SplitN(strings.TrimSuffix(filepath.Base(f), ".up.sql"), "_", 2)[1] == name {
				files = append(files, f)
			}
		}
		if len(files) != 1 {
			t.Fatalf("%s/*_%s.up.sql is %v, want one file", dir, name, files)
		}
		base := filepath.Base(files[0])
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		var recorded int
		if err := owner.QueryRowContext(t.Context(),
			"SELECT count(*) FROM schema_migrations WHERE version = $1 AND name = $2", version, base).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded != 1 {
			t.Errorf("%s: the ledger holds %d rows at version %d, want exactly one", base, recorded, version)
		}
	}

	var receipts, proposalRef, auditRef, index bool
	if err := owner.QueryRowContext(t.Context(), `SELECT
		to_regclass('platformkit_operations') IS NOT NULL,
		EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='change_proposals' AND column_name='operation_ref'),
		EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_events' AND column_name='operation_ref'),
		to_regclass('audit_events_operation') IS NOT NULL`).Scan(&receipts, &proposalRef, &auditRef, &index); err != nil {
		t.Fatal(err)
	}
	if !receipts || !proposalRef || !auditRef || !index {
		t.Errorf("receipts=%v proposal origin=%v audit origin=%v audit index=%v, want all present", receipts, proposalRef, auditRef, index)
	}
}
