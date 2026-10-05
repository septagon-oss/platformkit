package main

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// One version names one file across the whole composition, not one file per owner.
//
// The ledger keys on (owner, version), so two owners shipping one version each looks
// legal to the runner: migration_files.go refuses a repeated version *within* a source
// and says nothing about the file another owner numbered the same way. The installation
// that shows the two are not independent is the one that predates the split, where every
// file was owned by platformkit and every version had exactly one row:
// TestAnInstallationFromBeforeModulesOwnedTheirSQLUpgradesInPlace flattens this release's
// files under that one owner to build it, and a second file at a version that installation
// applied is a file no ledger row can place.
//
// The same collision reaches an operator. A file below the number some other owner has
// taken tends to be a file below what a running installation already applied, and that is
// a release nobody can install: the runner refuses a pending version below the applied
// head — "precedes applied version 43; append a new version" — and the application that
// boots by migrating refuses to boot. Nothing a fresh database does discovers it, because
// a fresh database applies every file in the order its own release lists.
//
// This is how this delivery's idempotency claims arrived at 000044: the file was written
// as 000042, and main took 42 for modules/content's body characters and 43 for the
// kernel's own tenant placement while it was in review.
//
// The versions a release carries are a fact of its file names, so the question is asked
// of the composed sources and of nothing else — no migration runs, and the answer a
// collision gets is the one a person can act on before a database is opened.
func TestEveryVersionInCompositionNamesOneFile(t *testing.T) {
	_, cfg := configure(t)
	by := map[int64]string{}
	seen := 0
	for _, source := range app.MigrationSources(compose(cfg).modules) {
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			t.Fatalf("read %s's migrations: %v", source.Owner, err)
		}
		for _, entry := range entries {
			digits, _, ok := strings.Cut(entry.Name(), "_")
			if !ok || !strings.HasSuffix(entry.Name(), ".up.sql") {
				t.Fatalf("%s/%s is not a migration file", source.Owner, entry.Name())
			}
			version, err := strconv.ParseInt(digits, 10, 64)
			if err != nil {
				t.Fatalf("%s/%s carries no version: %v", source.Owner, entry.Name(), err)
			}
			seen++
			if other, clash := by[version]; clash {
				t.Fatalf("%s and %s both ship version %d: one installation applied one file under "+
					"that number and can hold no row for the second", other, source.Owner+"/"+entry.Name(), version)
			}
			by[version] = source.Owner + "/" + entry.Name()
		}
	}
	if seen < 40 {
		t.Fatalf("the composition listed %d migration files, which is fewer than this app ships", seen)
	}
}
