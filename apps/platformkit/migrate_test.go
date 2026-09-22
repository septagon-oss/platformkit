package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// `platformkit migrate` has to migrate exactly what a boot would migrate, from the
// same composition, and then exit: it is the retry an operator reaches for after a
// contended file, and the door the rehearsal drives a candidate through. A command
// that migrated a subset of the sources would leave an installation half-migrated
// and report success, which is what this file exists to make impossible.
func TestMigrateCommandAppliesTheComposedSources(t *testing.T) {
	migrateURL, appURL := dbtest.URLs(t)
	owner := dbtest.Open(t, migrateURL)
	t.Cleanup(func() { owner.Close() })

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := fmt.Sprintf("server:\n  addr: \":0\"\n  public_host: \"rehearse.localhost\"\ndatabase:\n  url: %q\n  migrate_url: %q\nnats:\n  url: \"nats://127.0.0.1:4222\"\nlog:\n  level: \"info\"\n", appURL, migrateURL)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration the command is handed: %v", err)
	}

	// The same list every role's boot is handed, taken from the same door.
	sources := app.MigrationSources(compose(cfg).modules)
	want := expectedOwners(sources)
	if len(want) < 2 {
		t.Fatalf("the composition is supposed to select modules, not %v", want)
	}

	if err := migrate([]string{"--config", path}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	applied := appliedOwners(t, owner)
	if strings.Join(applied, ",") != strings.Join(want, ",") {
		t.Fatalf("the command applied %v, the composition selected %v", applied, want)
	}

	// The second run is the retry the command exists for, and a retry that re-ran
	// SQL would be the re-application the ledger's shape refuses.
	if err := migrate([]string{"--config", path}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if again := appliedOwners(t, owner); strings.Join(again, ",") != strings.Join(applied, ",") {
		t.Fatalf("the second run applied more: %v after %v", again, applied)
	}

	// --drain is the convergence a deployment does across two worker ticks: migrate,
	// let the backfill finish, migrate again. With nothing left to drain — the case
	// this composition is — it has to be a no-op and not an error, and it must not
	// re-apply a file either.
	if err := migrate([]string{"--config", path, "--drain"}); err != nil {
		t.Fatalf("migrate --drain: %v", err)
	}
	if again := appliedOwners(t, owner); strings.Join(again, ",") != strings.Join(applied, ",") {
		t.Fatalf("draining applied more: %v after %v", again, applied)
	}

	// The flag's default names the file a deployment has; a missing one is said in
	// config.Load's own words rather than answered by a run against nothing.
	if err := migrate([]string{"--config", filepath.Join(t.TempDir(), "absent.yaml")}); err == nil ||
		!strings.Contains(err.Error(), "absent.yaml") {
		t.Fatalf("a missing configuration returned %v", err)
	}
}

// appliedOwners is the ledger read back as the owner role, one entry per owner with
// how many files that owner applied, so a source that applied its first table and
// stopped is still caught.
func appliedOwners(t *testing.T, owner *sql.DB) []string {
	t.Helper()
	rows, err := owner.QueryContext(t.Context(),
		"SELECT owner, count(*) FROM schema_migrations GROUP BY owner ORDER BY owner")
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	defer rows.Close()
	var applied []string
	for rows.Next() {
		var name string
		var files int
		if err := rows.Scan(&name, &files); err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		applied = append(applied, fmt.Sprintf("%s=%d", name, files))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return applied
}

// expectedOwners names each selected source with the number of files it ships,
// which is the assertion that matters: not only which owners appear in the ledger,
// but that each one applied its whole history rather than its first table.
func expectedOwners(sources []db.MigrationSource) []string {
	var want []string
	for _, source := range sources {
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			panic("read the source: " + err.Error())
		}
		want = append(want, fmt.Sprintf("%s=%d", source.Owner, len(entries)))
	}
	sort.Strings(want)
	return want
}
