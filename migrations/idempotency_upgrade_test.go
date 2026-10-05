package migrations_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestAnInstallationWithTenantPlacementCanAddIdempotentCommands(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	prior := fstest.MapFS{}
	names, err := fs.Glob(migrations.Source.Files, "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// This is the historical kernel shipped with tenant placement (version 43),
	// before command claims existed. Later migrations must not enter this fixture.
	for _, name := range names {
		if name > "000043_tenant_app.up.sql" {
			continue
		}
		body, err := fs.ReadFile(migrations.Source.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "CREATE TABLE platformkit_idempotency") {
			continue
		}
		prior[name] = &fstest.MapFile{Data: body}
	}
	previous := migrations.Source
	previous.Files = prior
	if err := db.Migrate(t.Context(), adminURL, previous); err != nil {
		t.Fatalf("install the tenant-placement release: %v", err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatalf("upgrade an existing installation to idempotent commands: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	var exists bool
	if err := admin.QueryRowContext(t.Context(), "SELECT to_regclass('platformkit_idempotency') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("upgrade succeeded without installing command claims")
	}
}
