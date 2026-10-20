package internal_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// TestAnInstallationWithTheOperationColumnsTakesTheErasureFence migrates the audit source
// as an installation already holds it — every file but the erasure's grant and fence, so
// the operation columns are applied — and then migrates this tree's audit source over it.
// kit/db refuses a pending file whose version precedes one the ledger has already applied,
// so the erasure's files have to be appended past that head for the installation to boot.
func TestAnInstallationWithTheOperationColumnsTakesTheErasureFence(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	held := audit.Migrations
	files := fstest.MapFS{}
	names, err := fs.Glob(held.Files, "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.Contains(name, "_erasure_") {
			continue
		}
		data, err := fs.ReadFile(held.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	held.Files = files
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, held); err != nil {
		t.Fatalf("the audit source the installation holds: %v", err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, audit.Migrations); err != nil {
		t.Errorf("an installation with the operation columns cannot take this tree's audit source: %v", err)
	}
}
