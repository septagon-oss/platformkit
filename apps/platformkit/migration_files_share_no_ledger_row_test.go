package main

import (
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// Versions two owners both hold a file at, and both files applied somewhere.
//
// 35 is modules/audit's audit context beside modules/auth's passkeys; 50 is the
// kernel's tenant demo marker beside modules/task's revision column. Each arrived at a
// number another owner had taken while a branch was open, and each then shipped, which
// ends the moving: the ledger keys on (owner, version), so both owners keep their own
// row at that number, and either file can move only in a release nobody has applied.
// A third name here is the accident happening again; the answer is the number past the
// highest in the composition, for the file that has not shipped yet.
var sharedVersions = []int64{35, 50}

// One ledger row holds one file.
//
// No owner ships one version twice: the ledger holds one row per (owner, version) and
// kit/db refuses a repeated version inside a source before it reads history
// (migration_files.go, "invalid or repeated version"). And no version at or below the
// ceiling an installation reached under one owner is shipped by two owners: before each
// module owned its own SQL every file was applied as `platformkit`, and that ledger —
// up to the highest version any owner adopts back, the ceiling legacyLayout in
// app_test.go flattens to — holds one row per version whoever ships the file now.
//
// Above that ceiling two owners at one version are two rows and a release upgrades both,
// which is what sharedVersions records. Refusing it would mean moving a file somebody
// applied, and kit/db refuses that outright ("was applied but is missing from this
// release"), so the applied name is the fact and the collision is the residue. Either
// way the rule for a file not yet shipped is the same: continue past the highest number
// anywhere in the composition. The answer comes from the composed sources, before any
// database is opened.
func TestNoTwoFilesShareALedgerRow(t *testing.T) {
	_, cfg := configure(t)
	var ceiling int64
	var highest int64
	sources := app.MigrationSources(compose(cfg).modules)
	for _, source := range sources {
		for _, adoption := range source.Adopts {
			if adoption.Owner != "platformkit" {
				continue
			}
			for _, version := range adoption.Versions {
				ceiling = max(ceiling, version)
			}
		}
	}
	owner := map[int64]string{}
	seen := 0
	for _, source := range sources {
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
			highest = max(highest, version)
			file := source.Owner + "/" + entry.Name()
			first, clash := owner[version]
			if !clash {
				owner[version] = file
				continue
			}
			if strings.HasPrefix(first, source.Owner+"/") {
				t.Fatalf("%s and %s both ship version %d: the ledger holds one row per owner per version",
					first, file, version)
			}
			if version <= ceiling {
				t.Fatalf("%s and %s both ship version %d, at or below %d: an installation from before "+
					"the modules owned their SQL applied one file under that number and can hold no row "+
					"for the second", first, file, version, ceiling)
			}
			if !slices.Contains(sharedVersions, version) {
				t.Fatalf("%s and %s both ship version %d above the applied ceiling of %d, and that number "+
					"is named by no shared version: a file that has not shipped yet continues past %d",
					first, file, version, ceiling, highest)
			}
		}
	}
	if seen < 40 {
		t.Fatalf("the composition listed %d migration files, which is fewer than this app ships", seen)
	}
}
