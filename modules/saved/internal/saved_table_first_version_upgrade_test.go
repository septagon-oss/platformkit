package internal_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/saved"
)

// TestAnInstallationThatAppliedTheSavedTableUnderItsFirstVersionStillUpgrades asks
// the upgrade a real installation takes: it ran this module's SQL under version 75,
// the number the table was composed at, and now boots the release in this tree.
//
// Two owners may hold one version — the ledger keys on (owner, version), and only a
// repeated version inside one owner's own source is refused — so the installation
// below applied platformkit's 000075 and saved's 000075 in the same run. What the
// migrator refuses is an applied file its source no longer lists, whatever the bytes
// of the renamed one are: a migration is history, and history keeps its name.
func TestAnInstallationThatAppliedTheSavedTableUnderItsFirstVersionStillUpgrades(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	entries, err := fs.ReadDir(saved.Migrations.Files, ".")
	if err != nil || len(entries) == 0 {
		t.Fatalf("read saved's own migration source: %v (%d entries)", err, len(entries))
	}
	var name string
	var body []byte
	for _, entry := range entries {
		name = entry.Name()
		if body, err = fs.ReadFile(saved.Migrations.Files, name); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}

	// The release before: the same bytes under the version that installation wrote
	// into its ledger.
	applied := db.MigrationSource{Owner: "saved", Files: fstest.MapFS{
		"000075_saved.up.sql": &fstest.MapFile{Data: body, Mode: 0o444},
	}}
	if err := db.MigrateWithOptions(t.Context(), adminURL, db.MigrateOptions{}, migrations.Source, applied); err != nil {
		t.Fatalf("the installation that applied the saved table at version 75: %v", err)
	}
	if err := db.MigrateWithOptions(t.Context(), adminURL, db.MigrateOptions{}, migrations.Source, saved.Migrations); err != nil {
		t.Errorf("that installation booting this tree: %v", err)
	}
}
