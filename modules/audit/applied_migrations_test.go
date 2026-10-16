package audit_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// appliedAudit is what installations hold for the two audit files a later merge edited: the bytes
// first put on main, kept in testdata/applied, with the checksum the ledger recorded for each.
var appliedAudit = map[string]string{
	"000048_audit_history_append_only.up.sql": "cc72b6deb6d2a3aa5ec568e88ba7e40ec56e83a7711214ad14b9114df364c47a",
	"000049_audit_retention_marks.up.sql":     "0a8260d12bd8fb98e3babb522967a287e3219a682fcf762a3e28fd0b7a752564",
}

// firstLaterFoundationFile is the first kernel version an installation of that era did not have;
// the upgrade below applies the schema files from it, leaving a data file and its successors to the worker.
const firstLaterFoundationFile = 58

// TestAnInstallationThatAppliedTheShippedAuditFilesMigratesOnThisTree migrates a database as an
// installation of that era did — the kernel files it had and the audit files with their applied
// bytes — then migrates it with this tree's sources. The ledger refuses an applied file whose bytes
// changed, so a tree that edits either file fails here with "changed after it was applied".
func TestAnInstallationThatAppliedTheShippedAuditFilesMigratesOnThisTree(t *testing.T) {
	shipped := copyOf(t, audit.Migrations.Files, func(string) bool { return true })
	for name, sum := range appliedAudit {
		body, err := os.ReadFile(filepath.Join("testdata", "applied", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != sum {
			t.Fatalf("testdata/applied/%s has checksum %s, installations hold %s", name, got, sum)
		}
		shipped[name] = &fstest.MapFile{Data: body}
	}
	kernel := copyOf(t, migrations.Source.Files, func(name string) bool { return version(t, name) < firstLaterFoundationFile })
	installed := []db.MigrationSource{
		{Owner: migrations.Source.Owner, Files: kernel, RulesFrom: migrations.Source.RulesFrom},
		{Owner: audit.Migrations.Owner, Files: shipped, Adopts: audit.Migrations.Adopts, RulesFrom: audit.Migrations.RulesFrom},
	}

	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, installed...); err != nil {
		t.Fatalf("the installation's own migrate: %v", err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, audit.Migrations); err != nil {
		t.Fatalf("upgrading the installation to this tree: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	for name, sum := range appliedAudit {
		var checksum string
		if err := admin.QueryRowContext(t.Context(),
			"SELECT checksum FROM schema_migrations WHERE owner = 'audit' AND version = $1", version(t, name)).Scan(&checksum); err != nil {
			t.Fatalf("ledger row for audit/%s: %v", name, err)
		}
		if checksum != sum {
			t.Fatalf("ledger row for audit/%s carries %s after the upgrade, installations hold %s", name, checksum, sum)
		}
	}
}

// copyOf returns the files of fsys whose names keep accepts, as a map a test may overlay.
func copyOf(t *testing.T, fsys fs.FS, keep func(name string) bool) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") || !keep(entry.Name()) {
			continue
		}
		body, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: body}
	}
	return files
}

// version is the number a migration file's name starts with.
func version(t *testing.T, name string) int64 {
	t.Helper()
	number, _, _ := strings.Cut(name, "_")
	v, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		t.Fatalf("migration %s has no version: %v", name, err)
	}
	return v
}
